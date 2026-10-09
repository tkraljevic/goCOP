# Izmišljeni skup podataka za testove: sektor P

Sve u ovoj mapi je izmišljeno: osobe, telefoni, adrese, vode, dionice i
mjesta. Skup ima isti oblik kao datoteke uz bazu u `data/` (vidi
[predloške uvoza](../../docs/predlosci/README.md)), pa ga prvo punjenje čita
kao stvarne registre i imenik.

Na njemu rade automatske provjere djelatnika i ovlasti, uvoza i razmjene
među čvorovima. Zato se izvode svugdje, i na GitHubu, bez stvarnih podataka
i bez stvarnih imena. Test ga uključuje pozivom
`db.KoristiIzmisljenePodatke(t)`.

Provjere registara (Batina i Vukovar na Dunavu, kota nule iz elaborata,
cjelovitost registara) ostaju na stvarnim registrima u `data/` i pokreću se
prije svakog izdanja naredbom `make provjera-registara`.

## Organizacija

| Sektor | Branjeno područje | Dionice |
|---|---|---|
| P — Primjerski sliv | 1 Mali sliv Primjerica | P.1.1, P.1.2 (rijeka Primjerica), P.1.3 (kanal Probni) |
| P — Primjerski sliv | 2 Mali sliv Oglednica | P.2.1 (rijeka Oglednica) |
| E — Rubni sliv | 3 Mali sliv Rubnica | E.3.1, E.3.2 (rijeka Rubnica) |

Sektor E postoji zato da provjere ovlasti imaju dionicu izvan sektora P. Nosi
slovo iz raspona A–F jer program novu dionicu kroz obrazac prima samo sa
šifrom takvog sektora; dionice sektora P stižu samo prvim punjenjem.

Letve su Primjerovo, CS Probni, Oglednica i Rubno. CS Probni je i crpna
stanica i njezina letva istog imena. Dva potoka Graničnik razlikuje samo
pojašnjenje u zagradi.

## Djelatnici

Početna lozinka svih je `gocop2026`, kao u svakom sjemenu.

| Korisničko ime | Osoba | Dužnost |
|---|---|---|
| `pperic` | Pero Perić | Rukovoditelj sektora P |
| `aanic` | Ana Anić | Dežurna operaterka u COP-u (sektor P) |
| `iivic` | Ivo Ivić | globalni administrator i Dežurni operater u COP-u |
| `ivaivic` | Iva Ivić | Rukovoditeljica dionica P.1.1 i P.1.2 te branjenog područja 3 (sektor E) |
| `mmarkic` | Marko Markić | Vodočuvar dionice P.1.1 |

Račun `admin` dodaje se sam, kao pri svakom prvom punjenju.
