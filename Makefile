GOLANGCI_VERSION := v2.14.0
GREMLINS_VERSION := v0.6.0
QUALITY_ROOT := $(CURDIR)
QUALITY_OUT ?= $(QUALITY_ROOT)/.quality
QUALITY_BASELINE ?= $(QUALITY_ROOT)/quality/baseline.json

.PHONY: quality quality-tools quality-report quality-test quality-baseline quality-deep provjera-registara

quality-tools:
	GOBIN="$(QUALITY_ROOT)/bin/quality" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

quality-test:
	cd dev/quality && go test -race ./...

quality: quality-test
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -out "$(QUALITY_OUT)" -baseline "$(QUALITY_BASELINE)"

quality-report: quality-test
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -out "$(QUALITY_OUT)" -baseline "$(QUALITY_BASELINE)" -report-only

# Izričita odluka nakon pregleda diff-a izvještaja, nikad automatski u PR-u.
quality-baseline:
	@test "$(ACCEPT_BASELINE)" = "yes" || (echo 'Potrebno: make quality-baseline ACCEPT_BASELINE=yes'; exit 1)
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -out "$(QUALITY_OUT)" -baseline "$(QUALITY_BASELINE)" -record-baseline

# Isključivo zasebna čista kopija: Gremlins mijenja izvorne datoteke.
quality-deep:
	@test -n "$(MUTATION_ROOT)" || (echo 'Postavite MUTATION_ROOT na zasebnu čistu kopiju; vidi docs/CODE_QUALITY.md'; exit 1)
	GOBIN="$(QUALITY_ROOT)/bin/quality" go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)
	cd dev/quality && go run . -root "$(QUALITY_ROOT)" -mutation-root "$(MUTATION_ROOT)" -mutation-package "$(MUTATION_PACKAGE)" -out "$(QUALITY_OUT)"

# Provjere registara na stvarnim podacima (Batina i Vukovar na Dunavu, kota
# nule iz elaborata, cjelovitost registara), obavezno prije svakog izdanja.
# Trebaju cijeli izvoz registara u data/ u korijenu repozitorija: sections,
# watercourses, territories, section_territories, objekti_bp16 i
# organizacija (.json). Odakle je koja datoteka, piše u docs/INSTALACIJA.md,
# odjeljak „Podaci koji nisu u repozitoriju” (sections.json slaže alat
# prijepis-dionica, izvan repozitorija). Brojke u provjerama su brojke
# izvoza s kojim je napunjen prvi čvor; uz noviji izvoz razlike se
# pregledaju i brojke mijenjaju svjesno, s razlogom u commitu. Bez cijelog
# izvoza provjere padaju, ne preskaču se, i kažu koje datoteke nema; izvoz
# se tada prije izdanja vraća iz pričuve ili slaže iznova. Ostale provjere
# rade na izmišljenom skupu testdata/sektor-p i ne trebaju ništa. Kad je u
# data/ i baza čvora (gocop.db), testovi se usput pregledaju prema imenima iz nje:
# nijedan ne smije nositi stvarno ime ni korisničko ime. Baza se otvara samo
# za čitanje.
provjera-registara:
	GOCOP_REGISTRI=obavezno $(if $(wildcard data/gocop.db),GOCOP_STVARNA_BAZA="$(CURDIR)/data/gocop.db") go test -count=1 ./internal/db/
