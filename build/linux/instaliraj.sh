#!/usr/bin/env bash
# Postavlja ili nadograđuje goCOP čvor na Linuxu kao uslugu sustava.
#
#   sudo bash instaliraj.sh [-ime IME_ČVORA] PUT/gocop-linux-amd64
#   sudo bash instaliraj.sh -ukloni
#
# Uz program, u istoj mapi, moraju biti SHA256SUMS, SHA256SUMS.sig i
# gocop.service istog izdanja s GitHuba. Skripta:
#   1. provjeri potpis SHA256SUMS ključem izdanja te SHA-256 programa i
#      jedinice;
#   2. stvori korisnika sustava gocop, ako ga nema;
#   3. postavi program u /usr/local/bin/gocop; ako je ondje drugi program,
#      najprije zaustavi uslugu i spremi kopiju baze u /var/lib/gocop/kopije;
#   4. upiše ime čvora u /var/lib/gocop/gocop.toml (gocop -pripremi);
#   5. postavi jedinicu gocop.service iz izdanja, uključi je i pokrene;
#   6. pričeka da čvor odgovori na /zdravlje.
#
# Skripta se smije pokretati više puta: s istim programom ne mijenja ništa,
# s novim programom je nadogradnja. Podatke u /var/lib/gocop nikad ne briše,
# ni s -ukloni. Upute: docs/linux.md.

set -euo pipefail

PROGRAM=/usr/local/bin/gocop
PODACI=/var/lib/gocop
JEDINICA=/etc/systemd/system/gocop.service
KORISNIK=gocop
KOPIJA_ZADRZI=3

# Javni ključ izdanja (Ed25519), isti kao u internal/izdanje/izdanje.go
# (qFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=), u obliku za OpenSSL
KLJUC_IZDANJA='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAqFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=
-----END PUBLIC KEY-----'
DOMENA_POTPISA='goCOP izdanje v1'

greska() {
	echo "Greška: $*" >&2
	exit 1
}

poruka() {
	echo "→ $*"
}

upotreba() {
	sed -n '2,6p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

trebaju() {
	local n
	for n in "$@"; do
		command -v "$n" >/dev/null 2>&1 || greska "nedostaje naredba $n"
	done
}

# ---------- argumenti ----------

IME=""
UKLONI=0
IZVOR=""
while [ $# -gt 0 ]; do
	case "$1" in
	-ime | --ime)
		[ $# -ge 2 ] || greska "-ime traži ime čvora"
		IME=$2
		shift 2
		;;
	-ukloni | --ukloni)
		UKLONI=1
		shift
		;;
	-h | -pomoc | --pomoc | --help)
		upotreba 0
		;;
	-*)
		greska "nepoznata zastavica $1"
		;;
	*)
		[ -z "$IZVOR" ] || greska "navedite samo jedan program"
		IZVOR=$1
		shift
		;;
	esac
done

[ "$(id -u)" -eq 0 ] || greska "pokrenite kao root (sudo $0 …)"
trebaju systemctl install cmp

# ---------- uklanjanje ----------

if [ "$UKLONI" -eq 1 ]; then
	if [ -f "$JEDINICA" ]; then
		poruka "zaustavljam i isključujem uslugu gocop"
		systemctl disable --now gocop.service >/dev/null 2>&1 || true
		rm -f "$JEDINICA"
		systemctl daemon-reload
	fi
	if [ -f "$PROGRAM" ]; then
		poruka "uklanjam $PROGRAM"
		rm -f "$PROGRAM"
	fi
	echo
	echo "goCOP je uklonjen. Podaci su ostali u $PODACI, a korisnik $KORISNIK u sustavu."
	echo "U toj mapi je i ključ čvora (node-key): tko ga ima, predstavlja se kao ovaj čvor."
	echo "Ako ih više ne trebate (nakon što ste napravili kopiju), uklonite ih ručno:"
	echo "  sudo rm -rf $PODACI"
	echo "  sudo userdel $KORISNIK"
	exit 0
fi

# ---------- provjera izdanja ----------

[ -n "$IZVOR" ] || upotreba 2
[ -f "$IZVOR" ] || greska "nema datoteke $IZVOR"
[ "$(uname -m)" = x86_64 ] || greska "izdanja goCOP-a za Linux postoje samo za x86_64 (amd64), a ovo je $(uname -m)"
trebaju openssl sha256sum base64 runuser useradd

MAPA_IZDANJA=$(cd "$(dirname "$IZVOR")" && pwd)
IME_DATOTEKE=$(basename "$IZVOR")
ZBROJEVI=$MAPA_IZDANJA/SHA256SUMS
POTPIS=$MAPA_IZDANJA/SHA256SUMS.sig
[ -f "$ZBROJEVI" ] || greska "uz program nema SHA256SUMS (preuzmite ga uz isto izdanje)"
[ -f "$POTPIS" ] || greska "uz program nema SHA256SUMS.sig (preuzmite ga uz isto izdanje)"
[ -f "$MAPA_IZDANJA/gocop.service" ] || greska "uz program nema gocop.service (preuzmite je uz isto izdanje)"
case "$(openssl version)" in
OpenSSL\ [3-9].*) ;;
*) greska "za provjeru potpisa treba OpenSSL 3 ili noviji (ovdje: $(openssl version))" ;;
esac

PRIVREMENO=$(mktemp -d)
trap 'rm -rf "$PRIVREMENO"' EXIT

# Sve se provjerava i postavlja iz kopija u privremenoj mapi, koja je samo
# rootova, da se datoteka ne može zamijeniti između provjere i postavljanja.
cp "$ZBROJEVI" "$POTPIS" "$IZVOR" "$MAPA_IZDANJA/gocop.service" "$PRIVREMENO/"
ZBROJEVI=$PRIVREMENO/SHA256SUMS
POTPIS=$PRIVREMENO/SHA256SUMS.sig
IZVOR=$PRIVREMENO/$IME_DATOTEKE
JEDINICA_IZDANJA=$PRIVREMENO/gocop.service

poruka "provjeravam potpis izdanja"
printf '%s\n' "$KLJUC_IZDANJA" >"$PRIVREMENO/kljuc.pem"
{
	printf '%s\n' "$DOMENA_POTPISA"
	cat "$ZBROJEVI"
} >"$PRIVREMENO/poruka"
base64 -d "$POTPIS" >"$PRIVREMENO/potpis" 2>/dev/null || greska "SHA256SUMS.sig nije ispravnog oblika"
openssl pkeyutl -verify -pubin -inkey "$PRIVREMENO/kljuc.pem" -rawin \
	-in "$PRIVREMENO/poruka" -sigfile "$PRIVREMENO/potpis" >/dev/null 2>&1 ||
	greska "potpis SHA256SUMS ne odgovara ključu izdanja goCOP-a; ne postavljajte ovaj program"

provjeri_zbroj() {
	local ime=$1 savjet=$2
	poruka "provjeravam SHA-256 datoteke $ime"
	grep -E "^[0-9a-fA-F]{64}  \*?${ime//./\\.}\$" "$ZBROJEVI" >"$PRIVREMENO/zbroj" ||
		greska "u SHA256SUMS nema datoteke $ime ($savjet)"
	(cd "$PRIVREMENO" && sha256sum -c --status zbroj) ||
		greska "SHA-256 datoteke $ime ne odgovara izdanju; preuzmite je ponovno"
}
provjeri_zbroj "$IME_DATOTEKE" "ne preimenujte preuzeti program"
provjeri_zbroj gocop.service "izdanje je starije od ove skripte, pa jedinicu ne nosi"

# ---------- korisnik i mapa podataka ----------

if ! getent passwd "$KORISNIK" >/dev/null; then
	poruka "stvaram korisnika sustava $KORISNIK"
	NOLOGIN=$(command -v nologin || echo /usr/sbin/nologin)
	useradd --system --user-group --home-dir "$PODACI" --no-create-home \
		--shell "$NOLOGIN" --comment "goCOP čvor" "$KORISNIK"
fi
install -d -o "$KORISNIK" -g "$KORISNIK" -m 0750 "$PODACI"

# ---------- program ----------

PONOVO_POKRENI=0
if [ -f "$PROGRAM" ] && cmp -s "$IZVOR" "$PROGRAM"; then
	poruka "program je već postavljen ($("$PROGRAM" -version))"
else
	if systemctl is-active --quiet gocop.service; then
		poruka "zaustavljam uslugu gocop"
		systemctl stop gocop.service
	fi
	if [ -f "$PODACI/gocop.db" ]; then
		STARA=$([ -x "$PROGRAM" ] && "$PROGRAM" -version 2>/dev/null | awk '{print $2}' || true)
		OZNAKA=$(date +%Y%m%d-%H%M%S)-${STARA:-nepoznato}
		poruka "spremam kopiju baze u $PODACI/kopije/gocop-$OZNAKA.db"
		install -d -o "$KORISNIK" -g "$KORISNIK" -m 0700 "$PODACI/kopije"
		cp -p "$PODACI/gocop.db" "$PODACI/kopije/gocop-$OZNAKA.db"
		[ ! -f "$PODACI/gocop.db-wal" ] || cp -p "$PODACI/gocop.db-wal" "$PODACI/kopije/gocop-$OZNAKA.db-wal"
		# zadržava zadnje tri kopije
		ls -1t "$PODACI"/kopije/gocop-*.db 2>/dev/null | tail -n +$((KOPIJA_ZADRZI + 1)) |
			while read -r stara; do rm -f "$stara" "$stara-wal"; done
	fi
	poruka "postavljam $PROGRAM"
	install -o root -g root -m 0755 "$IZVOR" "$PROGRAM"
	PONOVO_POKRENI=1
fi
VERZIJA=$("$PROGRAM" -version)

# ---------- ime čvora i gocop.toml ----------

# -pripremi traži gocop.toml i u radnoj mapi, pa se pokreće iz mape podataka.
# Ime koje je već upisano ne mijenja.
(cd "$PODACI" && runuser -u "$KORISNIK" -- "$PROGRAM" -pripremi -db "$PODACI/gocop.db" ${IME:+-node "$IME"}) ||
	greska "gocop -pripremi nije uspio (provjerite ime čvora: mala slova, brojke i crtica, 3–40 znakova)"

# ---------- usluga ----------

if ! cmp -s "$JEDINICA_IZDANJA" "$JEDINICA"; then
	poruka "postavljam $JEDINICA"
	install -o root -g root -m 0644 "$JEDINICA_IZDANJA" "$JEDINICA"
	systemctl daemon-reload
	PONOVO_POKRENI=1
fi
systemctl enable --quiet gocop.service
if [ "$PONOVO_POKRENI" -eq 1 ]; then
	poruka "pokrećem uslugu gocop"
	systemctl restart gocop.service
elif ! systemctl is-active --quiet gocop.service; then
	poruka "pokrećem uslugu gocop"
	systemctl start gocop.service
fi

# ---------- provjera rada ----------

dohvati() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsS --max-time 3 "$1"
	else
		wget -qO- --timeout=3 "$1"
	fi
}

poruka "čekam da čvor odgovori na /zdravlje"
ODGOVOR=""
for _ in $(seq 1 60); do
	for adresa in http://127.0.0.1/zdravlje http://127.0.0.1:8080/zdravlje; do
		if ODGOVOR=$(dohvati "$adresa" 2>/dev/null) && [ -n "$ODGOVOR" ]; then
			break 2
		fi
	done
	ODGOVOR=""
	sleep 1
done
if [ -z "$ODGOVOR" ]; then
	echo "Čvor ne odgovara na /zdravlje. Zadnji redci dnevnika:" >&2
	journalctl -u gocop.service -n 30 --no-pager >&2 || true
	exit 1
fi

WEB=${adresa%/zdravlje}
echo
echo "$VERZIJA radi kao usluga gocop: $ODGOVOR ($WEB)"
echo "Podaci i gocop.toml: $PODACI"
echo "Dnevnik: journalctl -u gocop -f"

# Svjež čvor pri svakom pokretanju ispiše u dnevnik kod za /postavljanje
POKRETANJE=$(systemctl show -p InvocationID --value gocop.service 2>/dev/null || true)
KOD=""
if [ -n "$POKRETANJE" ]; then
	KOD=$(journalctl "_SYSTEMD_INVOCATION_ID=$POKRETANJE" --no-pager -o cat 2>/dev/null |
		sed -n 's/.*Postavljanje: svjež čvor.*kod=\([A-Za-z0-9-]*\).*/\1/p' | tail -n 1 || true)
fi
if [ -n "$KOD" ]; then
	echo
	echo "Čvor još nije postavljen. Na ovom računalu otvorite $WEB/postavljanje,"
	echo "a s računala u lokalnoj mreži http://<adresa ovog računala>${WEB#http://127.0.0.1}/postavljanje?kod=$KOD"
	echo "Do tada čvor ne izlažite internetu (docs/linux.md, SECURITY.md)."
fi
