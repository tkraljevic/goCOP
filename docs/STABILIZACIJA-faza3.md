# Faza 3: kandidati (CRAP > 30, coverage < 80 %), mjerenje mastera 713d9df

Izvor: `dev/quality` na Linuxu (go1.27.1). Bez `cmd/gocop/main.go`, `internal/repository/apply.go` i `dev/quality`, izostavljenih namjerno. Ukupno 629 funkcija, od toga 183 kritične. Poredano po CRAP-u. Prednost imaju kritične funkcije i one u `internal/service`.

| # | Funkcija | CC | Coverage | CRAP | Kritična |
|---:|---|---:|---:|---:|:---:|
| 1 | `internal/importer/bp16/journals.go:242` · `RunJournals` | 82 | 0.0 % | 6806.0 | da |
| 2 | `internal/service/zid_service.go:363` · `(*opisivac).opisi` | 104 | 27.0 % | 4314.4 |  |
| 3 | `internal/db/seed.go:64` · `SeedInitialData` | 59 | 0.0 % | 3540.0 |  |
| 4 | `internal/web/prognoze_sazetak.go:344` · `(*PrognozeHandler).listSazetka` | 58 | 0.0 % | 3422.0 |  |
| 5 | `internal/importer/bp16/bp16.go:368` · `Run` | 55 | 0.0 % | 3080.0 | da |
| 6 | `internal/service/akt_service.go:395` · `(*AktService).primatelji` | 52 | 0.0 % | 2756.0 | da |
| 7 | `internal/prognoza/provjera.go:45` · `ProvjeriUnatrag` | 49 | 0.0 % | 2450.0 | da |
| 8 | `internal/importer/csvlevels/csvlevels.go:104` · `Run` | 47 | 0.0 % | 2256.0 | da |
| 9 | `internal/pdfw/dodatak.go:102` · `Dodaj` | 47 | 0.0 % | 2256.0 |  |
| 10 | `internal/posta/proba.go:107` · `PokreniProbniEWS` | 65 | 22.8 % | 2012.4 |  |
| 11 | `internal/web/brojke.go:144` · `(*Server).izbrojiPodatke` | 44 | 0.0 % | 1980.0 |  |
| 12 | `internal/importer/bp16/obilasci.go:100` · `RunObilasci` | 43 | 0.0 % | 1892.0 | da |
| 13 | `internal/web/handlers_prognoze.go:867` · `uzduzniProfili` | 42 | 0.0 % | 1806.0 |  |
| 14 | `internal/web/handlers_sections_pages.go:309` · `(*SectionsHandler).ShowSectionForm` | 40 | 0.0 % | 1640.0 |  |
| 15 | `internal/service/akt_service.go:132` · `(*AktService).Pripremi` | 38 | 0.0 % | 1482.0 | da |
| 16 | `internal/prognoza/osvjezavanje.go:573` · `(*Osvjezivac).dnevno` | 40 | 4.4 % | 1436.0 | da |
| 17 | `internal/service/reading_service.go:379` · `(*ReadingService).FieldOverview` | 34 | 0.0 % | 1190.0 | da |
| 18 | `internal/web/slika_lanca.go:177` · `slikaLanca` | 34 | 0.0 % | 1190.0 |  |
| 19 | `internal/prognoza/namjestanje.go:207` · `NamjestiLetvu` | 32 | 0.0 % | 1056.0 | da |
| 20 | `internal/web/pricuvno_izvoz.go:71` · `knjigaPricuvno` | 32 | 0.0 % | 1056.0 |  |
| 21 | `internal/importer/ugovor/ugovor.go:540` · `Run` | 31 | 0.0 % | 992.0 | da |
| 22 | `internal/web/handlers_stations_pages.go:391` · `(*StationsHandler).podaciLetve` | 31 | 0.0 % | 992.0 |  |
| 23 | `internal/web/handlers_prognoze.go:406` · `(*PrognozeHandler).podaci` | 30 | 0.0 % | 930.0 |  |
| 24 | `internal/web/handlers_prognoze.go:589` · `(*PrognozeHandler).opisiLetve` | 30 | 0.0 % | 930.0 |  |
| 25 | `internal/service/akt_posta.go:640` · `(*AktService).UsporediImenik` | 29 | 0.0 % | 870.0 |  |
| 26 | `internal/web/handlers_vrijeme.go:158` · `(*VrijemeHandler).kisa` | 29 | 3.6 % | 783.1 |  |
| 27 | `internal/service/akt_posta.go:322` · `(*AktService).PosaljiNaZnanje` | 27 | 0.0 % | 756.0 |  |
| 28 | `internal/importer/ugovor/ugovor.go:108` · `(*Contract).parseTroskovnik` | 26 | 0.0 % | 702.0 | da |
| 29 | `internal/repository/arhiva_repo.go:134` · `(*ArhivaRepository).Pregled` | 26 | 0.0 % | 702.0 |  |
| 30 | `internal/service/vodocuvar_service.go:262` · `(*VodocuvarService).Spremi` | 26 | 0.0 % | 702.0 |  |
| 31 | `internal/uvoz/his2000/posao.go:40` · `(*Posao).Uvezi` | 26 | 0.0 % | 702.0 | da |
| 32 | `internal/service/reading_service.go:143` · `(*ReadingService).validate` | 25 | 0.0 % | 650.0 | da |
| 33 | `internal/uvoz/godisnjak/uvoz.go:45` · `Uvezi` | 25 | 0.0 % | 650.0 | da |
| 34 | `internal/web/handlers_prognoze.go:1015` · `(*PrognozeHandler).usca` | 25 | 0.0 % | 650.0 |  |
| 35 | `internal/prognoza/osvjezavanje.go:480` · `(*Osvjezivac).vrhoviIzDnevnog` | 26 | 4.9 % | 607.1 | da |
| 36 | `internal/db/stations_seed.go:103` · `buildStationDrafts` | 24 | 0.0 % | 600.0 |  |
| 37 | `internal/service/maintenance_service.go:77` · `(*MaintenanceService).AddWater` | 24 | 0.0 % | 600.0 |  |
| 38 | `internal/service/organization_service.go:191` · `(*OrgService).SaveContractor` | 24 | 0.0 % | 600.0 |  |
| 39 | `internal/uvoz/hvpovijest/hvpovijest.go:56` · `Preuzmi` | 24 | 0.0 % | 600.0 | da |
| 40 | `internal/web/handlers_readings.go:1163` · `(*ReadingsHandler).HandleZalijepiPregled` | 24 | 0.0 % | 600.0 | da |
| 41 | `internal/web/handlers_stations_pages.go:518` · `(*StationsHandler).ShowStationForm` | 23 | 0.0 % | 552.0 |  |
| 42 | `internal/service/akt_posta.go:932` · `(*AktService).ZadaniPotpis` | 22 | 0.0 % | 506.0 |  |
| 43 | `internal/web/handlers_uvoz_izvora.go:281` · `(*UvozHandler).PreuzmiHidroView` | 22 | 0.0 % | 506.0 | da |
| 44 | `internal/db/sections_link.go:447` · `(*Linker).linkStructures` | 29 | 18.0 % | 492.1 |  |
| 45 | `internal/prognoza/pricuvno.go:96` · `PricuvniIzracun` | 21 | 0.0 % | 462.0 | da |
| 46 | `internal/service/reading_service.go:266` · `(*ReadingService).Overview` | 21 | 0.0 % | 462.0 | da |
| 47 | `internal/ulaganje/ulaganje.go:218` · `(*Pregled).Ulozi` | 21 | 0.0 % | 462.0 |  |
| 48 | `internal/web/handlers_sections_pages.go:120` · `(*SectionsHandler).napuniDionicu` | 21 | 0.0 % | 462.0 |  |
| 49 | `internal/web/handlers_uvoz_profila.go:61` · `(*UvozHandler).PregledUvozaProfila` | 21 | 0.0 % | 462.0 | da |
| 50 | `internal/service/akt_posta.go:860` · `(*AktService).AdreseZaPismo` | 20 | 0.0 % | 420.0 |  |
| 51 | `internal/service/dezurstva_service.go:207` · `(*JournalService).Obracun` | 20 | 0.0 % | 420.0 |  |
| 52 | `internal/web/handlers_organization.go:541` · `(*OrgHandler).ShowContractorForm` | 20 | 0.0 % | 420.0 |  |
| 53 | `internal/web/handlers_slivovi.go:133` · `(*SlivoviHandler).ShowSlivovi` | 20 | 0.0 % | 420.0 |  |
| 54 | `internal/web/handlers_territories_csv.go:87` · `(*TerritoriesHandler).HandleImportTerritoriesCSV` | 20 | 0.0 % | 420.0 |  |
| 55 | `internal/web/karta_geo.go:101` · `slozKartuObuhvata` | 20 | 0.0 % | 420.0 |  |
| 56 | `internal/arhiva/gradnja.go:1052` · `krivulje` | 21 | 6.6 % | 380.8 | da |
| 57 | `internal/peers/subscriptions.go:54` · `(Subscription).Label` | 19 | 0.0 % | 380.0 | da |
| 58 | `internal/peers/subscriptions.go:376` · `(*Service).PurgeChannel` | 19 | 0.0 % | 380.0 | da |
| 59 | `internal/service/akt_service.go:666` · `(*AktService).zakljuciOvjeru` | 19 | 0.0 % | 380.0 | da |
| 60 | `internal/web/handlers_arhiva_csv.go:217` · `(*ReadingsHandler).HandleArhivaUvoz` | 19 | 0.0 % | 380.0 |  |
| 61 | `internal/web/handlers_ocitanja_csv.go:504` · `(*ReadingsHandler).HandleOcitanjaPotvrda` | 19 | 0.0 % | 380.0 |  |
| 62 | `internal/web/handlers_subscriptions.go:111` · `predlogPretplate` | 19 | 0.0 % | 380.0 |  |
| 63 | `internal/web/handlers_uvoz_profila.go:199` · `(*UvozHandler).UpisiProfile` | 19 | 0.0 % | 380.0 | da |
| 64 | `internal/web/karta_geo.go:23` · `geoTocke` | 19 | 0.0 % | 380.0 |  |
| 65 | `internal/web/kisa_slivova.go:52` · `(*Server).ShowKisaSlivova` | 19 | 0.0 % | 380.0 |  |
| 66 | `internal/prognoza/prenesene.go:286` · `(*Osvjezivac).prenesene` | 28 | 25.9 % | 346.7 | da |
| 67 | `internal/models/terms.go:133` · `(OrgTerms).Get` | 18 | 0.0 % | 342.0 |  |
| 68 | `internal/prognoza/lanac.go:534` · `NamjestiLanac` | 18 | 0.0 % | 342.0 | da |
| 69 | `internal/service/postavljanje.go:35` · `(*UserService).PrviAdministrator` | 18 | 0.0 % | 342.0 |  |
| 70 | `internal/uvoz/his2000/posao.go:140` · `(*Posao).Niz` | 18 | 0.0 % | 342.0 | da |
| 71 | `internal/web/handlers_field.go:47` · `(*FieldHandler).ShowField` | 18 | 0.0 % | 342.0 |  |
| 72 | `internal/importer/bp16/journals.go:183` · `classify` | 17 | 0.0 % | 306.0 | da |
| 73 | `internal/importer/ugovor/ugovor.go:406` · `(*index).resolve` | 17 | 0.0 % | 306.0 | da |
| 74 | `internal/pdfw/dodatak.go:379` · `Potpisi` | 17 | 0.0 % | 306.0 |  |
| 75 | `internal/posta/sanducic.go:56` · `SveMape` | 17 | 0.0 % | 306.0 |  |
| 76 | `internal/service/prijepis_ugrozeno.go:31` · `(*TerritoryService).RazrijesiUgrozeno` | 17 | 0.0 % | 306.0 |  |
| 77 | `internal/uvoz/his2000/mjerenja.go:36` · `ProcitajMjerenja` | 17 | 0.0 % | 306.0 | da |
| 78 | `internal/web/handlers_organization.go:439` · `(*OrgHandler).HandleImportContractorsCSV` | 17 | 0.0 % | 306.0 |  |
| 79 | `internal/web/handlers_uvoz_niza.go:716` · `(*UvozHandler).UpisiUvoz` | 17 | 0.0 % | 306.0 | da |
| 80 | `internal/models/hidro.go:114` · `NazivIzvora` | 16 | 0.0 % | 272.0 |  |
| 81 | `internal/posta/imenik.go:32` · `Imenik` | 16 | 0.0 % | 272.0 |  |
| 82 | `internal/prognoza/dnevna.go:601` · `OborineOkoSada` | 16 | 0.0 % | 272.0 | da |
| 83 | `internal/repository/journal_repo.go:705` · `(*JournalRepository).ArhivirajDnevnik` | 16 | 0.0 % | 272.0 |  |
| 84 | `internal/service/prijava_service.go:365` · `(*PrijavaService).Urudzbiraj` | 16 | 0.0 % | 272.0 |  |
| 85 | `internal/uvoz/godisnjak/godisnjak.go:63` · `citajStranicu` | 16 | 0.0 % | 272.0 | da |
| 86 | `internal/web/handlers_organization.go:304` · `(*OrgHandler).HandleImportCSV` | 16 | 0.0 % | 272.0 |  |
| 87 | `internal/web/prognoze_sazetak.go:96` · `postajeSazetka` | 16 | 0.0 % | 272.0 |  |
| 88 | `cmd/gocop-postava/traka.go:286` · `(*traka).osvjezi` | 15 | 0.0 % | 240.0 |  |
| 89 | `internal/models/modules.go:129` · `ResolveModules` | 15 | 0.0 % | 240.0 |  |
| 90 | `internal/postava/nadogradnja.go:103` · `(Ugradnja).PripremiIzMape` | 15 | 0.0 % | 240.0 |  |
| 91 | `internal/repository/section_repo.go:252` · `noveVezeDionice` | 15 | 0.0 % | 240.0 |  |
| 92 | `internal/repository/user_repo.go:266` · `(*UserRepository).ListUsersDomena` | 15 | 0.0 % | 240.0 | da |
| 93 | `internal/service/akt_posta.go:537` · `(*AktService).PosaljiPismo` | 15 | 0.0 % | 240.0 |  |
| 94 | `internal/service/obracun_service.go:71` · `(*ObracunService).SpremiBlagdan` | 15 | 0.0 % | 240.0 |  |
| 95 | `internal/service/prijava_service.go:262` · `(*PrijavaService).Objavi` | 15 | 0.0 % | 240.0 |  |
| 96 | `internal/uvoz/godisnjak/godisnjak.go:372` · `(Tablica).Provjeri` | 15 | 0.0 % | 240.0 | da |
| 97 | `internal/web/handlers_readings.go:688` · `(*ReadingsHandler).koritoUzGraf` | 15 | 0.0 % | 240.0 | da |
| 98 | `internal/web/uzduzni_jutro.go:144` · `(*PrognozeHandler).mjesecno` | 15 | 0.0 % | 240.0 |  |
| 99 | `internal/ulaganje/zaboravljanje.go:88` · `Zaboravi` | 16 | 7.8 % | 216.6 |  |
| 100 | `internal/db/watercourses_seed.go:41` · `seedWatercourses` | 14 | 0.0 % | 210.0 |  |
| 101 | `internal/peers/archive.go:85` · `(*Service).ImportFile` | 14 | 0.0 % | 210.0 | da |
| 102 | `internal/prognoza/osvjezavanje.go:411` · `(*Osvjezivac).dnevniModeliZaDanas` | 14 | 0.0 % | 210.0 | da |
| 103 | `internal/service/journal_service.go:212` · `(*JournalService).waterLevels` | 14 | 0.0 % | 210.0 |  |
| 104 | `internal/service/journal_service.go:333` · `(*JournalService).AddEntry` | 14 | 0.0 % | 210.0 |  |
| 105 | `internal/service/reading_service.go:189` · `(*ReadingService).Create` | 14 | 0.0 % | 210.0 | da |
| 106 | `internal/uvoz/godisnjak/godisnjak.go:296` · `poStupcima` | 14 | 0.0 % | 210.0 | da |
| 107 | `internal/web/handlers_arhiva_csv.go:300` · `(*ReadingsHandler).HandleArhivaPotvrda` | 14 | 0.0 % | 210.0 |  |
| 108 | `internal/web/handlers_ocitanja_csv.go:438` · `(*ReadingsHandler).HandleOcitanjaUvoz` | 14 | 0.0 % | 210.0 |  |
| 109 | `internal/web/handlers_prognoze.go:1302` · `(*PrognozeHandler).mjerenoUnatrag` | 14 | 0.0 % | 210.0 |  |
| 110 | `internal/web/handlers_readings.go:1071` · `prorijedi` | 14 | 0.0 % | 210.0 | da |
| 111 | `internal/web/handlers_sections_popis_izvoz.go:30` · `(*SectionsHandler).IzvoziPopisDionica` | 14 | 0.0 % | 210.0 |  |
| 112 | `internal/web/handlers_telemetrija.go:201` · `(*TelemetrijaHandler).SpremiMLetvu` | 14 | 0.0 % | 210.0 |  |
| 113 | `internal/web/vodocuvar_pdf.go:36` · `nacrtajPriloge` | 16 | 9.6 % | 205.0 |  |
| 114 | `internal/db/sections_link.go:590` · `(*Linker).linkTerritories` | 16 | 11.1 % | 195.8 |  |
| 115 | `internal/web/server.go:161` · `templateFuncs` | 57 | 65.4 % | 191.8 |  |
| 116 | `internal/db/structures_seed.go:34` · `seedStructures` | 13 | 0.0 % | 182.0 |  |
| 117 | `internal/ikona/ikona.go:46` · `Slika` | 13 | 0.0 % | 182.0 |  |
| 118 | `internal/importer/bp16/bp16.go:666` · `noviObjekt` | 13 | 0.0 % | 182.0 | da |
| 119 | `internal/peers/archive.go:24` · `(*Service).ExportFile` | 13 | 0.0 % | 182.0 | da |
| 120 | `internal/posta/sanducic.go:228` · `(ewsMessage).pismo` | 13 | 0.0 % | 182.0 |  |
| 121 | `internal/repository/akti_repo.go:172` · `(*AktiRepository).ListAkti` | 13 | 0.0 % | 182.0 |  |
| 122 | `internal/repository/akti_repo.go:619` · `(*AktiRepository).PopuniOtiskeRacunaPoste` | 13 | 0.0 % | 182.0 |  |
| 123 | `internal/repository/arhiva_repo.go:1148` · `(*ArhivaRepository).GodisnjeVrijednosti` | 13 | 0.0 % | 182.0 |  |
| 124 | `internal/service/akt_service.go:825` · `(*AktService).UcitajSkenirani` | 13 | 0.0 % | 182.0 | da |
| 125 | `internal/service/dezurstva_service.go:352` · `(*JournalService).ObracunOsobe` | 13 | 0.0 % | 182.0 |  |
| 126 | `internal/service/kisomjer_service.go:143` · `validateKisomjer` | 13 | 0.0 % | 182.0 |  |
| 127 | `internal/service/maintenance_service.go:189` · `(*MaintenanceService).SaveItem` | 13 | 0.0 % | 182.0 |  |
| 128 | `internal/service/objava_akta.go:182` · `(*VodocuvarService).UpisiIzAkta` | 13 | 0.0 % | 182.0 |  |
| 129 | `internal/web/handlers_izvori.go:201` · `(*IzvoriHandler).SpremiIzvor` | 13 | 0.0 % | 182.0 |  |
| 130 | `internal/web/handlers_maintenance.go:82` · `(*MaintenanceHandler).area` | 13 | 0.0 % | 182.0 |  |
| 131 | `internal/web/handlers_paket.go:156` · `(*StationsHandler).PregledPaketa` | 13 | 0.0 % | 182.0 |  |
| 132 | `internal/web/handlers_stations.go:393` · `(*StationsHandler).HandleUpdateStationAPI` | 13 | 0.0 % | 182.0 |  |
| 133 | `internal/web/handlers_stations.go:715` · `(*StationsHandler).namjestiTelemetriju` | 13 | 0.0 % | 182.0 |  |
| 134 | `internal/web/handlers_telemetrija.go:138` · `(*TelemetrijaHandler).Spremi` | 13 | 0.0 % | 182.0 |  |
| 135 | `internal/web/handlers_uvoz_krivulja.go:172` · `(*UvozHandler).UpisiKrivulje` | 13 | 0.0 % | 182.0 | da |
| 136 | `internal/web/handlers_uvoz_niza.go:479` · `(*UvozHandler).popuniPregled` | 13 | 0.0 % | 182.0 | da |
| 137 | `internal/web/uzduzni_jutro.go:57` · `(*PrognozeHandler).uobicajeno` | 13 | 0.0 % | 182.0 |  |
| 138 | `internal/service/dezurstva_service.go:60` · `(*JournalService).SpremiDezurstvo` | 27 | 41.3 % | 174.4 |  |
| 139 | `internal/prognoza/dnevna.go:457` · `DnevneZnacajke` | 20 | 27.5 % | 172.7 | da |
| 140 | `internal/javnivodostaji/hidroview.go:99` · `(*HidroView).ocitanja` | 14 | 9.8 % | 157.8 |  |
| 141 | `internal/db/sections_link.go:537` · `(*Linker).upisiNoviZapis` | 12 | 0.0 % | 156.0 |  |
| 142 | `internal/db/seed.go:413` · `seedOrganization` | 12 | 0.0 % | 156.0 |  |
| 143 | `internal/db/watercourses_seed.go:147` · `linkWatercourses` | 12 | 0.0 % | 156.0 |  |
| 144 | `internal/hydro/section.go:192` · `ParseEmbankmentData` | 12 | 0.0 % | 156.0 |  |
| 145 | `internal/models/user.go:357` · `(*UserPermissions).VodiSkladista` | 12 | 0.0 % | 156.0 | da |
| 146 | `internal/prognoza/dnevna.go:235` · `SastaviBezKise` | 12 | 0.0 % | 156.0 | da |
| 147 | `internal/prognoza/dnevna.go:662` · `DnevneOborine` | 12 | 0.0 % | 156.0 | da |
| 148 | `internal/prognoza/dnevna.go:718` · `DnevniIzArhive` | 12 | 0.0 % | 156.0 | da |
| 149 | `internal/repository/akti_repo.go:876` · `(*AktiRepository).DeleteAktTrajno` | 12 | 0.0 % | 156.0 |  |
| 150 | `internal/repository/arhiva_repo.go:645` · `(*ArhivaRepository).Sazetak` | 12 | 0.0 % | 156.0 |  |
| 151 | `internal/repository/arhiva_repo.go:1046` · `(*ArhivaRepository).PoDesetljecima` | 12 | 0.0 % | 156.0 |  |
| 152 | `internal/service/akt_service.go:601` · `(*AktService).Spremi` | 12 | 0.0 % | 156.0 | da |
| 153 | `internal/service/journal_service.go:106` · `(*JournalService).SaveJournal` | 12 | 0.0 % | 156.0 |  |
| 154 | `internal/service/organization_service.go:115` · `(*OrgService).SaveArea` | 12 | 0.0 % | 156.0 |  |
| 155 | `internal/service/vodocuvar_service.go:569` · `(*VodocuvarService).Upisi` | 12 | 0.0 % | 156.0 |  |
| 156 | `internal/web/handlers_maintenance.go:118` · `(*MaintenanceHandler).pageFor` | 12 | 0.0 % | 156.0 |  |
| 157 | `internal/web/valovi_pamcenje.go:58` · `(*valoviPamcenje).Valovi` | 12 | 0.0 % | 156.0 |  |
| 158 | `internal/service/izvjesca_service.go:105` · `(*IzvjescaService).vodostajiU7` | 14 | 10.3 % | 155.2 |  |
| 159 | `internal/web/handlers_prognoze_podaci.go:186` · `(*PrognozeHandler).mjerenoZaIzvoz` | 13 | 9.1 % | 140.0 |  |
| 160 | `internal/prognoza/osvjezavanje.go:156` · `(*Osvjezivac).Osvjezi` | 63 | 73.2 % | 139.6 | da |
| 161 | `internal/web/izvjesce_xlsx.go:187` · `(listLetve).historijat` | 23 | 40.3 % | 135.6 |  |
| 162 | `cmd/gocop-postava/main.go:31` · `main` | 11 | 0.0 % | 132.0 |  |
| 163 | `cmd/gocop/arhiva_razmjena.go:385` · `stanjeRazmjene` | 11 | 0.0 % | 132.0 |  |
| 164 | `internal/db/stations_seed.go:207` · `seedStations` | 11 | 0.0 % | 132.0 |  |
| 165 | `internal/models/akt.go:336` · `(Akt).NositeljFunkcije` | 11 | 0.0 % | 132.0 |  |
| 166 | `internal/models/sluzba.go:45` · `SluzbaLabel` | 11 | 0.0 % | 132.0 |  |
| 167 | `internal/repository/arhiva_repo.go:336` · `(*ArhivaRepository).Profili` | 11 | 0.0 % | 132.0 |  |
| 168 | `internal/repository/arhiva_repo.go:403` · `(*ArhivaRepository).Krivulje` | 11 | 0.0 % | 132.0 |  |
| 169 | `internal/repository/arhiva_repo.go:492` · `(*ArhivaRepository).SpojDosezi` | 11 | 0.0 % | 132.0 |  |
| 170 | `internal/repository/dezurstva_repo.go:140` · `(*JournalRepository).PlanoviOsobe` | 11 | 0.0 % | 132.0 |  |
| 171 | `internal/repository/mts_repo.go:269` · `(*MtsRepository).osigurajOblike` | 11 | 0.0 % | 132.0 |  |
| 172 | `internal/repository/mts_repo.go:668` · `(*MtsRepository).UpotrebaVrste` | 11 | 0.0 % | 132.0 |  |
| 173 | `internal/repository/prijava_repo.go:272` · `(*PrijavaRepository).List` | 11 | 0.0 % | 132.0 |  |
| 174 | `internal/repository/territory_repo.go:415` · `(*TerritoryRepository).DeleteMunicipality` | 11 | 0.0 % | 132.0 |  |
| 175 | `internal/service/akt_service.go:274` · `(*AktService).UrediTekst` | 11 | 0.0 % | 132.0 | da |
| 176 | `internal/service/akt_service.go:1045` · `(*AktService).SpremiZig` | 11 | 0.0 % | 132.0 | da |
| 177 | `internal/service/akt_service.go:1112` · `(*AktService).SpremiPotpisSliku` | 11 | 0.0 % | 132.0 | da |
| 178 | `internal/service/episode_service.go:176` · `(*EpisodeService).Rebuild` | 11 | 0.0 % | 132.0 | da |
| 179 | `internal/service/organization_service.go:62` · `(*OrgService).SaveSector` | 11 | 0.0 % | 132.0 |  |
| 180 | `internal/service/reading_service.go:120` · `(*ReadingService).CanEdit` | 11 | 0.0 % | 132.0 | da |
| 181 | `internal/service/vodocuvar_service.go:206` · `(*VodocuvarService).prilike` | 11 | 0.0 % | 132.0 |  |
| 182 | `internal/web/brojke.go:268` · `(*Server).velicinePodataka` | 11 | 0.0 % | 132.0 |  |
| 183 | `internal/web/handlers_admin.go:71` · `(*AdminHandler).ShowAdmin` | 11 | 0.0 % | 132.0 |  |
| 184 | `internal/web/handlers_arhiva_csv.go:23` · `(*ReadingsHandler).HandleArhivaIzvoz` | 11 | 0.0 % | 132.0 |  |
| 185 | `internal/web/handlers_dbmaint.go:105` · `(*DBMaintHandler).ShowMaintenance` | 11 | 0.0 % | 132.0 |  |
| 186 | `internal/web/handlers_dbmaint.go:254` · `(*DBMaintHandler).HandleImport` | 11 | 0.0 % | 132.0 |  |
| 187 | `internal/web/handlers_prognoze.go:287` · `(*PrognozeHandler).ShowPostavke` | 11 | 0.0 % | 132.0 |  |
| 188 | `internal/web/handlers_readings.go:1252` · `(*ReadingsHandler).HandleZalijepiPotvrda` | 11 | 0.0 % | 132.0 | da |
| 189 | `internal/web/prognoza_citac.go:44` · `(*CitacPrognoza).ZaLetvu` | 11 | 0.0 % | 132.0 |  |
| 190 | `internal/web/prognoze_sazetak.go:279` · `recenicaMadjara` | 11 | 0.0 % | 132.0 |  |
| 191 | `internal/web/server.go:1533` · `(*Server).authMiddleware` | 18 | 30.0 % | 129.1 | da |
| 192 | `internal/web/prijava_pdf.go:415` · `uMjestu` | 13 | 14.3 % | 119.4 |  |
| 193 | `internal/db/watercourses_seed.go:103` · `watercourseIndex` | 10 | 0.0 % | 110.0 |  |
| 194 | `internal/importer/bp16/bp16.go:141` · `LoadEnv` | 10 | 0.0 % | 110.0 | da |
| 195 | `internal/models/hidro.go:62` · `NazivVelicine` | 10 | 0.0 % | 110.0 |  |
| 196 | `internal/models/roles.go:92` · `(Role).Rank` | 10 | 0.0 % | 110.0 | da |
| 197 | `internal/pdfw/dodatak.go:435` · `tekstIzRjecnika` | 10 | 0.0 % | 110.0 |  |
| 198 | `internal/prognoza/dnevna.go:983` · `PrognozirajDnevno` | 10 | 0.0 % | 110.0 | da |
| 199 | `internal/repository/mts_repo.go:828` · `(*MtsRepository).osigurajPotrebeIzInventura` | 10 | 0.0 % | 110.0 |  |
| 200 | `internal/repository/prijava_repo.go:146` · `(*PrijavaRepository).Objavi` | 10 | 0.0 % | 110.0 |  |
| 201 | `internal/repository/reading_repo.go:108` · `(*ReadingRepository).List` | 10 | 0.0 % | 110.0 | da |
| 202 | `internal/repository/reading_repo.go:448` · `(*ReadingRepository).ListForGauges` | 10 | 0.0 % | 110.0 | da |
| 203 | `internal/repository/structure_repo.go:75` · `(*StructureRepository).ListStructures` | 10 | 0.0 % | 110.0 |  |
| 204 | `internal/repository/user_repo.go:552` · `(*UserRepository).DeleteUser` | 10 | 0.0 % | 110.0 | da |
| 205 | `internal/repository/vodocuvar_repo.go:161` · `(*VodocuvarRepository).List` | 10 | 0.0 % | 110.0 |  |
| 206 | `internal/service/akt_service.go:961` · `(*AktService).List` | 10 | 0.0 % | 110.0 | da |
| 207 | `internal/service/dezurstva_service.go:440` · `(*JournalService).PreuzmiDezurstvo` | 10 | 0.0 % | 110.0 |  |
| 208 | `internal/service/journal_service.go:409` · `(*JournalService).SetTaskStatus` | 10 | 0.0 % | 110.0 |  |
| 209 | `internal/service/maintenance_service.go:46` · `(*MaintenanceService).LinkWater` | 10 | 0.0 % | 110.0 |  |
| 210 | `internal/service/mts_service.go:889` · `(*MtsService).tablica` | 10 | 0.0 % | 110.0 |  |
| 211 | `internal/service/objava_akta.go:128` · `(*JournalService).OtvoreniDnevniciOdrzavanja` | 10 | 0.0 % | 110.0 |  |
| 212 | `internal/service/prijava_service.go:112` · `(*PrijavaService).Spremi` | 10 | 0.0 % | 110.0 |  |
| 213 | `internal/service/reading_service.go:71` · `(*ReadingService).CanRecordStation` | 10 | 0.0 % | 110.0 | da |
| 214 | `internal/service/structure_service.go:108` · `(*StructureService).Update` | 10 | 0.0 % | 110.0 |  |
| 215 | `internal/service/territory_service.go:61` · `GenerateProtectedAreaText` | 10 | 0.0 % | 110.0 |  |
| 216 | `internal/service/vodocuvar_service.go:145` · `(*VodocuvarService).pripremiZa` | 10 | 0.0 % | 110.0 |  |
| 217 | `internal/service/vodocuvar_service.go:365` · `(*VodocuvarService).ZadajZadatak` | 10 | 0.0 % | 110.0 |  |
| 218 | `internal/ulaganje/ulaganje.go:151` · `Pripremi` | 10 | 0.0 % | 110.0 |  |
| 219 | `internal/uvoz/godisnjak/godisnjak.go:334` · `brojeviURetku` | 10 | 0.0 % | 110.0 | da |
| 220 | `internal/web/handlers_dbmaint.go:204` · `(*DBMaintHandler).HandleExport` | 10 | 0.0 % | 110.0 |  |
| 221 | `internal/web/handlers_dnevnik_cop_ovjera.go:33` · `(*JournalsHandler).HandleOvjeraCOP` | 10 | 0.0 % | 110.0 |  |
| 222 | `internal/web/handlers_journals.go:389` · `(*JournalsHandler).HandleSaveJournal` | 10 | 0.0 % | 110.0 |  |
| 223 | `internal/web/handlers_sections.go:126` · `sectionFromForm` | 10 | 0.0 % | 110.0 |  |
| 224 | `internal/web/handlers_slivovi.go:307` · `slivoviJSON` | 10 | 0.0 % | 110.0 |  |
| 225 | `internal/web/handlers_slivovi.go:350` · `(*SlivoviHandler).ShowKisomjerForm` | 10 | 0.0 % | 110.0 |  |
| 226 | `internal/web/handlers_structures.go:137` · `(*StructuresHandler).ShowStructure` | 10 | 0.0 % | 110.0 |  |
| 227 | `internal/web/handlers_telemetrija.go:74` · `(*TelemetrijaHandler).Prikazi` | 10 | 0.0 % | 110.0 |  |
| 228 | `internal/web/handlers_ulaganje.go:91` · `(*UvozHandler).UloziOcitanja` | 10 | 0.0 % | 110.0 |  |
| 229 | `internal/web/handlers_uvoz_niza.go:399` · `(*UvozHandler).pogledajPosao` | 10 | 0.0 % | 110.0 | da |
| 230 | `internal/prognoza/zamjenske.go:136` · `(*Osvjezivac).zamjenaNaKraju` | 12 | 13.0 % | 106.7 | da |
| 231 | `internal/arhiva/gradnja.go:727` · `mogucaVrijednost` | 16 | 30.0 % | 103.8 | da |
| 232 | `internal/prognoza/osvjezavanje.go:1098` · `(*Osvjezivac).uDrugojVelicini` | 12 | 14.8 % | 101.0 | da |
| 233 | `internal/arhiva/gradnja.go:899` · `poravnanjaProfila` | 11 | 10.3 % | 98.2 | da |
| 234 | `internal/models/models.go:322` · `normirajZemlju` | 19 | 40.0 % | 97.0 |  |
| 235 | `internal/web/handlers_prognoze_izvoz.go:430` · `listMetode` | 29 | 56.8 % | 96.8 |  |
| 236 | `internal/peers/peers.go:838` · `(*Service).exchange` | 44 | 70.6 % | 93.3 | da |
| 237 | `internal/db/schema.go:1284` · `migrateSchema` | 41 | 68.8 % | 91.9 |  |
| 238 | `internal/arhiva/gradnja.go:653` · `promjeneKote` | 11 | 12.9 % | 90.9 | da |
| 239 | `cmd/gocop-postava/traka.go:105` · `(*traka).pocetak` | 9 | 0.0 % | 90.0 |  |
| 240 | `internal/arhiva/gradnja.go:1318` · `PostaviIzvor` | 9 | 0.0 % | 90.0 | da |
| 241 | `internal/arhiva/ulaz.go:46` · `Letve` | 9 | 0.0 % | 90.0 | da |
| 242 | `internal/importer/bp16/bp16.go:44` · `(HTTPSource).Items` | 9 | 0.0 % | 90.0 | da |
| 243 | `internal/importer/ugovor/ugovor.go:715` · `newWatercourse` | 9 | 0.0 % | 90.0 | da |
| 244 | `internal/javnivodostaji/arso.go:62` · `(ARSO).Ocitanja` | 9 | 0.0 % | 90.0 |  |
| 245 | `internal/models/hidro.go:39` · `JedinicaVelicine` | 9 | 0.0 % | 90.0 |  |
| 246 | `internal/models/spatial.go:392` · `(SectionPart).Compose` | 9 | 0.0 % | 90.0 |  |
| 247 | `internal/peers/archive.go:152` · `(*Service).ChannelsFor` | 9 | 0.0 % | 90.0 | da |
| 248 | `internal/prognoza/namjestanje.go:55` · `doGranice` | 9 | 0.0 % | 90.0 | da |
| 249 | `internal/prognoza/osvjezavanje.go:1016` · `KrivuljeIzArhive` | 9 | 0.0 % | 90.0 | da |
| 250 | `internal/repository/arhiva_repo.go:272` · `(*ArhivaRepository).trajanje` | 9 | 0.0 % | 90.0 |  |
| 251 | `internal/repository/arhiva_repo.go:991` · `(*ArhivaRepository).Brojke` | 9 | 0.0 % | 90.0 |  |
| 252 | `internal/repository/contractor_repo.go:171` · `(*OrgRepository).DeleteContractor` | 9 | 0.0 % | 90.0 |  |
| 253 | `internal/repository/prijava_repo.go:186` · `(*PrijavaRepository).Urudzbiraj` | 9 | 0.0 % | 90.0 |  |
| 254 | `internal/sadrzaj/sadrzaj.go:279` · `(*Spremiste).ObrisiKanal` | 9 | 0.0 % | 90.0 |  |
| 255 | `internal/service/akt_posta.go:776` · `najboljiKontakt` | 9 | 0.0 % | 90.0 |  |
| 256 | `internal/service/auth_service.go:205` · `(*AuthService).AuthenticateSessionView` | 9 | 0.0 % | 90.0 | da |
| 257 | `internal/service/dezurstva_service.go:152` · `(*JournalService).MakniDezurstvo` | 9 | 0.0 % | 90.0 |  |
| 258 | `internal/service/episode_service.go:44` · `(*EpisodeService).Declare` | 9 | 0.0 % | 90.0 | da |
| 259 | `internal/service/kisomjer_service.go:94` · `(*KisomjerService).PomakniKisomjere` | 9 | 0.0 % | 90.0 |  |
| 260 | `internal/service/objava_akta.go:80` · `(*ObjavaAkta).Objavi` | 9 | 0.0 % | 90.0 |  |
| 261 | `internal/service/vodocuvar_service.go:537` · `(*VodocuvarService).Kalendar` | 9 | 0.0 % | 90.0 |  |
| 262 | `internal/weather/geocode.go:32` · `(*Geokoder).Nadji` | 9 | 0.0 % | 90.0 |  |
| 263 | `internal/web/handlers_journals.go:322` · `(*JournalsHandler).ShowJournalForm` | 9 | 0.0 % | 90.0 |  |
| 264 | `internal/web/handlers_maintenance.go:254` · `(*MaintenanceHandler).showImport` | 9 | 0.0 % | 90.0 |  |
| 265 | `internal/web/handlers_organization.go:676` · `readLogo` | 9 | 0.0 % | 90.0 |  |
| 266 | `internal/web/handlers_sections.go:62` · `(*SectionsHandler).ShowSections` | 9 | 0.0 % | 90.0 |  |
| 267 | `internal/web/handlers_stations.go:511` · `parseStationIDs` | 9 | 0.0 % | 90.0 |  |
| 268 | `internal/web/handlers_telemetrija.go:111` · `(*TelemetrijaHandler).letve` | 9 | 0.0 % | 90.0 |  |
| 269 | `internal/web/server.go:1990` · `(*Server).smijeUrediLetvu` | 9 | 0.0 % | 90.0 |  |
| 270 | `internal/web/slika_lanca.go:68` · `vrstaCvora` | 9 | 0.0 % | 90.0 |  |
| 271 | `internal/web/handlers_watercourses_pages.go:197` · `(*WatercoursesHandler).napuniLetve` | 11 | 15.4 % | 84.3 |  |
| 272 | `internal/service/journal_service.go:661` · `(*JournalService).IspraviPrijepis` | 13 | 25.0 % | 84.3 |  |
| 273 | `internal/web/handlers_prognoze_izvoz_grafovi.go:136` · `(*PrognozeHandler).mjerenoSatno` | 10 | 10.5 % | 81.6 |  |
| 274 | `internal/postava/postava.go:287` · `(*Postava).Instaliraj` | 25 | 56.0 % | 78.2 |  |
| 275 | `internal/peers/peers.go:239` · `(*Service).PublicAddress` | 17 | 41.2 % | 75.8 | da |
| 276 | `cmd/gocop/prognoza_razmjena.go:208` · `prorjedjujIzdanja` | 8 | 0.0 % | 72.0 |  |
| 277 | `internal/hidroview/hidroview.go:231` · `(*Klijent).Oprema` | 8 | 0.0 % | 72.0 |  |
| 278 | `internal/importer/csvlevels/csvlevels.go:430` · `variants` | 8 | 0.0 % | 72.0 | da |
| 279 | `internal/importer/csvlevels/csvlevels.go:454` · `preferStructure` | 8 | 0.0 % | 72.0 | da |
| 280 | `internal/importer/ugovor/ugovor.go:186` · `(*Contract).parseLocations` | 8 | 0.0 % | 72.0 | da |
| 281 | `internal/importer/ugovor/ugovor.go:235` · `classifyHeader` | 8 | 0.0 % | 72.0 | da |
| 282 | `internal/javnivodostaji/javnivodostaji.go:164` · `(*Client).SektoriPostaja` | 8 | 0.0 % | 72.0 |  |
| 283 | `internal/models/journal.go:242` · `(JournalSheet).WeatherText` | 8 | 0.0 % | 72.0 |  |
| 284 | `internal/models/journal.go:466` · `EntryKindLabel` | 8 | 0.0 % | 72.0 |  |
| 285 | `internal/models/maintenance.go:134` · `(MaintainedWater).PlanPosition` | 8 | 0.0 % | 72.0 |  |
| 286 | `internal/models/user.go:242` · `(*User).VidiVodocuvarskiDnevnik` | 8 | 0.0 % | 72.0 | da |
| 287 | `internal/obracun/obracun.go:51` · `(RadnoVrijeme).Naziv` | 8 | 0.0 % | 72.0 |  |
| 288 | `internal/peers/subscriptions.go:172` · `(Wants).DrziDanaZa` | 8 | 0.0 % | 72.0 | da |
| 289 | `internal/prognoza/hydroinfo.go:132` · `Dohvati` | 8 | 0.0 % | 72.0 | da |
| 290 | `internal/prognoza/lanac.go:415` · `namjestiRacun` | 8 | 0.0 % | 72.0 | da |
| 291 | `internal/repository/akti_repo.go:671` · `(*AktiRepository).sazetakVrijediOd` | 8 | 0.0 % | 72.0 |  |
| 292 | `internal/repository/arhiva_repo.go:714` · `(*ArhivaRepository).izostriSatnim` | 8 | 0.0 % | 72.0 |  |
| 293 | `internal/repository/arhiva_repo.go:855` · `(*ArhivaRepository).SpojSredina` | 8 | 0.0 % | 72.0 |  |
| 294 | `internal/repository/arhiva_repo.go:895` · `(*ArhivaRepository).SpojNajbolji` | 8 | 0.0 % | 72.0 |  |
| 295 | `internal/repository/izvjesca_repo.go:132` · `(*IzvjescaRepository).List` | 8 | 0.0 % | 72.0 |  |
| 296 | `internal/repository/user_repo.go:959` · `(*UserRepository).MarkLogin` | 8 | 0.0 % | 72.0 | da |
| 297 | `internal/repository/watercourse_repo.go:66` · `(*WatercourseRepository).ListWatercourses` | 8 | 0.0 % | 72.0 |  |
| 298 | `internal/service/akt_service.go:304` · `nazivVode` | 8 | 0.0 % | 72.0 | da |
| 299 | `internal/service/akt_service.go:638` · `(*AktService).Ovjeri` | 8 | 0.0 % | 72.0 | da |
| 300 | `internal/service/akt_service.go:907` · `(*AktService).aktKojiSePrekida` | 8 | 0.0 % | 72.0 | da |
| 301 | `internal/service/episode_service.go:109` · `(*EpisodeService).End` | 8 | 0.0 % | 72.0 | da |
| 302 | `internal/service/episode_service.go:138` · `(*EpisodeService).dopuniPrag` | 8 | 0.0 % | 72.0 | da |
| 303 | `internal/service/izvjesca_service.go:46` · `(*IzvjescaService).SmijeVidjeti` | 8 | 0.0 % | 72.0 |  |
| 304 | `internal/service/journal_service.go:154` · `(*JournalService).NewSheet` | 8 | 0.0 % | 72.0 |  |
| 305 | `internal/service/journal_service.go:253` · `(*JournalService).UpdateSheet` | 8 | 0.0 % | 72.0 |  |
| 306 | `internal/service/objava_akta.go:23` · `(*JournalService).AktivnaObrana` | 8 | 0.0 % | 72.0 |  |
| 307 | `internal/service/objava_akta.go:147` · `(*JournalService).NapomenaNadzoraIzAkta` | 8 | 0.0 % | 72.0 |  |
| 308 | `internal/service/organization_service.go:283` · `(*OrgService).DeleteContractor` | 8 | 0.0 % | 72.0 |  |
| 309 | `internal/service/potpis_service.go:205` · `(*PotpisService).Simulirani` | 8 | 0.0 % | 72.0 |  |
| 310 | `internal/service/station_service.go:143` · `(*StationService).CreateStation` | 8 | 0.0 % | 72.0 | da |
| 311 | `internal/service/territory_service.go:308` · `(*TerritoryService).SpremiSluzbu` | 8 | 0.0 % | 72.0 |  |
| 312 | `internal/service/vodocuvar_service.go:176` · `(*VodocuvarService).zadaciNaListu` | 8 | 0.0 % | 72.0 |  |
| 313 | `internal/service/vodocuvar_service.go:445` · `(*VodocuvarService).Parafiraj` | 8 | 0.0 % | 72.0 |  |
| 314 | `internal/ulaganje/citanje.go:163` · `ocitanjaZaUlaganje` | 8 | 0.0 % | 72.0 |  |
| 315 | `internal/uvoz/godisnjak/godisnjak.go:166` · `metapodaci` | 8 | 0.0 % | 72.0 | da |
| 316 | `internal/uvoz/godisnjak/godisnjak.go:234` · `pokupiDan` | 8 | 0.0 % | 72.0 | da |
| 317 | `internal/uvoz/his2000/posao.go:316` · `ucitaj` | 8 | 0.0 % | 72.0 | da |
| 318 | `internal/uvoz/hvpovijest/hvpovijest.go:228` · `dohvati` | 8 | 0.0 % | 72.0 | da |
| 319 | `internal/uvoz/izvori/izvori.go:354` · `izZIP` | 8 | 0.0 % | 72.0 | da |
| 320 | `internal/web/brojke.go:111` · `(*Server).ShowBrojke` | 8 | 0.0 % | 72.0 |  |
| 321 | `internal/web/handlers_akti.go:228` · `smijeLetvu` | 8 | 0.0 % | 72.0 |  |
| 322 | `internal/web/handlers_dnevnik_cop_ovjera.go:12` · `(*JournalsHandler).potpisnikCOP` | 8 | 0.0 % | 72.0 |  |
| 323 | `internal/web/handlers_dnevnik_cop_ovjera.go:83` · `(*JournalsHandler).IzvoziDnevnikPDF` | 8 | 0.0 % | 72.0 |  |
| 324 | `internal/web/handlers_maintenance.go:297` · `groupWaters` | 8 | 0.0 % | 72.0 |  |
| 325 | `internal/web/handlers_ocitanja_csv.go:72` · `(*ReadingsHandler).HandleOcitanjaIzvoz` | 8 | 0.0 % | 72.0 |  |
| 326 | `internal/web/handlers_organization.go:89` · `(*OrgHandler).ShowOrganization` | 8 | 0.0 % | 72.0 |  |
| 327 | `internal/web/handlers_organization.go:370` · `(*OrgHandler).ShowContractors` | 8 | 0.0 % | 72.0 |  |
| 328 | `internal/web/handlers_organization.go:598` · `(*OrgHandler).HandleSaveContractor` | 8 | 0.0 % | 72.0 |  |
| 329 | `internal/web/handlers_paket.go:236` · `(*StationsHandler).UgradiPaket` | 8 | 0.0 % | 72.0 |  |
| 330 | `internal/web/handlers_prijave.go:46` · `(*PrijaveHandler).Sken` | 8 | 0.0 % | 72.0 |  |
| 331 | `internal/web/handlers_prognoze.go:1120` · `opisDnevnogCilja` | 8 | 0.0 % | 72.0 |  |
| 332 | `internal/web/handlers_prognoze_izvoz_grafovi.go:502` · `(*PrognozeHandler).krajnosti` | 8 | 0.0 % | 72.0 |  |
| 333 | `internal/web/handlers_sections_pages.go:195` · `(*SectionsHandler).IzvoziDionicu` | 8 | 0.0 % | 72.0 |  |
| 334 | `internal/web/handlers_slivovi.go:273` · `(*SlivoviHandler).rijekeJSON` | 8 | 0.0 % | 72.0 |  |
| 335 | `internal/web/handlers_stations.go:764` · `(*StationsHandler).vjerodajniceTelemetrije` | 8 | 0.0 % | 72.0 |  |
| 336 | `internal/web/handlers_stations.go:802` · `(*StationsHandler).dopuniJavnuAdresu` | 8 | 0.0 % | 72.0 |  |
| 337 | `internal/web/handlers_stations_pages.go:609` · `odabraniNiz` | 8 | 0.0 % | 72.0 |  |
| 338 | `internal/web/handlers_territories_pages.go:81` · `(*TerritoriesHandler).ShowMunicipalityForm` | 8 | 0.0 % | 72.0 |  |
| 339 | `internal/web/handlers_ulaganje.go:26` · `(*UvozHandler).zahtjevIzObrasca` | 8 | 0.0 % | 72.0 |  |
| 340 | `internal/web/handlers_ulaganje.go:205` · `(*UvozHandler).MakniSirotana` | 8 | 0.0 % | 72.0 |  |
| 341 | `internal/web/handlers_uvoz_krivulja.go:50` · `(*UvozHandler).PregledUvozaKrivulja` | 8 | 0.0 % | 72.0 | da |
| 342 | `internal/web/handlers_uvoz_niza.go:433` · `(*UvozHandler).PregledUvoza` | 8 | 0.0 % | 72.0 | da |
| 343 | `internal/web/handlers_uvoz_niza.go:850` · `(*UvozHandler).izdavanje` | 8 | 0.0 % | 72.0 | da |
| 344 | `internal/web/handlers_watercourses.go:435` · `(*WatercoursesHandler).HandleAssignStationWatercourseAPI` | 8 | 0.0 % | 72.0 |  |
| 345 | `internal/web/izvjesce.go:477` · `(IzvjesceLetve).koritoOpis` | 8 | 0.0 % | 72.0 |  |
| 346 | `internal/web/server.go:1848` · `(*Server).PostaviIzvor` | 8 | 0.0 % | 72.0 |  |
| 347 | `internal/web/server.go:2017` · `(*Server).koteNuleLetve` | 8 | 0.0 % | 72.0 |  |
| 348 | `internal/arhiva/gradnja.go:1480` · `spojiJedan` | 47 | 78.3 % | 69.6 | da |
| 349 | `internal/web/handlers_readings.go:521` · `(*ReadingsHandler).podaciOcitanja` | 43 | 77.3 % | 64.6 | da |
| 350 | `internal/web/izvjesce_xlsx.go:279` · `(listLetve).ocitanja` | 31 | 67.6 % | 63.7 |  |
| 351 | `internal/web/handlers_readings.go:959` · `(*ReadingsHandler).protokLetve` | 10 | 18.8 % | 63.6 | da |
| 352 | `internal/javnivodostaji/javnivodostaji.go:746` · `(*Uvoznik).Preuzmi` | 30 | 66.7 % | 63.3 |  |
| 353 | `internal/arhiva/paket.go:851` · `Ugradi` | 35 | 72.8 % | 59.5 | da |
| 354 | `internal/peers/cop.go:404` · `(*Service).UveziCop` | 36 | 74.6 % | 57.1 | da |
| 355 | `internal/web/handlers_territories_vode.go:44` · `(*TerritoriesHandler).vodniIzbor` | 8 | 9.1 % | 56.1 |  |
| 356 | `cmd/gocop/arhiva_razmjena.go:344` · `(*razmjenaArhive).vrti` | 7 | 0.0 % | 56.0 |  |
| 357 | `internal/importer/bp16/bp16.go:703` · `opisObjekta` | 7 | 0.0 % | 56.0 | da |
| 358 | `internal/importer/bp16/journals.go:36` · `(HTTPSource).Users` | 7 | 0.0 % | 56.0 | da |
| 359 | `internal/importer/csvlevels/csvlevels.go:361` · `loadGauges` | 7 | 0.0 % | 56.0 | da |
| 360 | `internal/importer/ugovor/ugovor.go:373` · `(*index).pick` | 7 | 0.0 % | 56.0 | da |
| 361 | `internal/javnivodostaji/javnivodostaji.go:120` · `(*Uvoznik).SPopisa` | 7 | 0.0 % | 56.0 |  |
| 362 | `internal/javnivodostaji/javnivodostaji.go:481` · `(*Uvoznik).rezervaDanubeHIS` | 7 | 0.0 % | 56.0 |  |
| 363 | `internal/javnivodostaji/javnivodostaji.go:562` · `(*Uvoznik).Pokreni` | 7 | 0.0 % | 56.0 |  |
| 364 | `internal/ledger/ledger.go:546` · `(*Recorder).Recent` | 7 | 0.0 % | 56.0 |  |
| 365 | `internal/mletva/mletva.go:149` · `(*Klijent).Stranica` | 7 | 0.0 % | 56.0 |  |
| 366 | `internal/mletva/mletva.go:168` · `(*Klijent).dohvati` | 7 | 0.0 % | 56.0 |  |
| 367 | `internal/models/akt.go:279` · `(Akt).Predmet` | 7 | 0.0 % | 56.0 |  |
| 368 | `internal/models/akt.go:510` · `SkupinaLabel` | 7 | 0.0 % | 56.0 |  |
| 369 | `internal/models/akt.go:529` · `(Primatelj).Vrijedi` | 7 | 0.0 % | 56.0 |  |
| 370 | `internal/models/maintenance.go:101` · `(MaintainedWater).OrderLabel` | 7 | 0.0 % | 56.0 |  |
| 371 | `internal/models/models.go:668` · `(Station).NajviseIzmjereno` | 7 | 0.0 % | 56.0 |  |
| 372 | `internal/models/models.go:747` · `(Station).NajnizeIzmjereno` | 7 | 0.0 % | 56.0 |  |
| 373 | `internal/models/models.go:766` · `(Station).NajnizeRekonstruirano` | 7 | 0.0 % | 56.0 |  |
| 374 | `internal/models/mts.go:275` · `nazivTerena` | 7 | 0.0 % | 56.0 |  |
| 375 | `internal/models/mts.go:486` · `OznakaSredstva` | 7 | 0.0 % | 56.0 |  |
| 376 | `internal/models/opseg.go:25` · `OpsegDnevnika` | 7 | 0.0 % | 56.0 |  |
| 377 | `internal/models/prijava.go:114` · `PodaciFotoaparata` | 7 | 0.0 % | 56.0 |  |
| 378 | `internal/models/reading.go:97` · `FlowMethodLabel` | 7 | 0.0 % | 56.0 |  |
| 379 | `internal/models/structure.go:84` · `StructureKindLabel` | 7 | 0.0 % | 56.0 |  |
| 380 | `internal/obracun/obracun.go:101` · `(Razred).Kratko` | 7 | 0.0 % | 56.0 |  |
| 381 | `internal/obracun/pravila.go:66` · `(Pravilo).Opis` | 7 | 0.0 % | 56.0 |  |
| 382 | `internal/peers/peers.go:1073` · `(*Service).RunAutoSync` | 7 | 0.0 % | 56.0 | da |
| 383 | `internal/peers/uloge.go:26` · `(*Service).UcitajUloge` | 7 | 0.0 % | 56.0 | da |
| 384 | `internal/peers/uloge.go:56` · `(*Service).PostaviUloge` | 7 | 0.0 % | 56.0 | da |
| 385 | `internal/posta/posta.go:443` · `Ispitaj` | 7 | 0.0 % | 56.0 |  |
| 386 | `internal/prognoza/operater.go:94` · `razinaZnacajke` | 7 | 0.0 % | 56.0 | da |
| 387 | `internal/prognoza/provjera.go:258` · `zagladiPromasaje` | 7 | 0.0 % | 56.0 | da |
| 388 | `internal/repository/akti_repo.go:222` · `(*AktiRepository).DeleteAkt` | 7 | 0.0 % | 56.0 |  |
| 389 | `internal/repository/biljeska_repo.go:85` · `(*BiljeskaRepository).Krajnosti` | 7 | 0.0 % | 56.0 |  |
| 390 | `internal/repository/contractor_repo.go:31` · `(*OrgRepository).ListContractors` | 7 | 0.0 % | 56.0 |  |
| 391 | `internal/repository/drugi_korak_repo.go:395` · `(*DrugiKorakRepository).StanjeRezervnih` | 7 | 0.0 % | 56.0 |  |
| 392 | `internal/repository/journal_repo.go:128` · `(*JournalRepository).ListJournals` | 7 | 0.0 % | 56.0 |  |
| 393 | `internal/repository/journal_repo.go:671` · `(*JournalRepository).ListCOPJournals` | 7 | 0.0 % | 56.0 |  |
| 394 | `internal/repository/maintenance_repo.go:55` · `(*MaintenanceRepository).ListWaters` | 7 | 0.0 % | 56.0 |  |
| 395 | `internal/repository/mts_repo.go:243` · `(*MtsRepository).OsigurajKatalog` | 7 | 0.0 % | 56.0 |  |
| 396 | `internal/repository/mts_repo.go:303` · `(*MtsRepository).ListSkladista` | 7 | 0.0 % | 56.0 |  |
| 397 | `internal/repository/mts_repo.go:476` · `(*MtsRepository).Stanje` | 7 | 0.0 % | 56.0 |  |
| 398 | `internal/repository/mts_repo.go:541` · `(*MtsRepository).osigurajPodrucjaTerena` | 7 | 0.0 % | 56.0 |  |
| 399 | `internal/repository/mts_repo.go:627` · `(*MtsRepository).ListPopisi` | 7 | 0.0 % | 56.0 |  |
| 400 | `internal/repository/prijava_repo.go:328` · `(*PrijavaRepository).Delete` | 7 | 0.0 % | 56.0 |  |
| 401 | `internal/repository/section_repo.go:75` · `(*SectionRepository).ListSections` | 7 | 0.0 % | 56.0 |  |
| 402 | `internal/repository/territory_repo.go:505` · `(*TerritoryRepository).DeleteSettlement` | 7 | 0.0 % | 56.0 |  |
| 403 | `internal/repository/territory_repo.go:530` · `(*TerritoryRepository).GetSectionsAffectedByTerritory` | 7 | 0.0 % | 56.0 |  |
| 404 | `internal/service/akt_posta.go:279` · `(*AktService).AdresatiAkta` | 7 | 0.0 % | 56.0 |  |
| 405 | `internal/service/akt_service.go:89` · `ProvjeriPotpis` | 7 | 0.0 % | 56.0 | da |
| 406 | `internal/service/akt_service.go:985` · `(*AktService).Obrisi` | 7 | 0.0 % | 56.0 | da |
| 407 | `internal/service/akt_service.go:1194` · `(*AktService).SpremiTemu` | 7 | 0.0 % | 56.0 | da |
| 408 | `internal/service/dezurstva_service.go:131` · `(*JournalService).PotvrdiDezurstvo` | 7 | 0.0 % | 56.0 |  |
| 409 | `internal/service/episode_service.go:85` · `(*EpisodeService).Raise` | 7 | 0.0 % | 56.0 | da |
| 410 | `internal/service/journal_service.go:715` · `(*JournalService).ObrisiDnevnik` | 7 | 0.0 % | 56.0 |  |
| 411 | `internal/service/objava_akta.go:54` · `TekstObjave` | 7 | 0.0 % | 56.0 |  |
| 412 | `internal/service/objava_akta.go:109` · `(*JournalService).ZapisIzAkta` | 7 | 0.0 % | 56.0 |  |
| 413 | `internal/service/prijava_service.go:174` · `(*PrijavaService).DodajSliku` | 7 | 0.0 % | 56.0 |  |
| 414 | `internal/service/reading_service.go:46` · `hasAnyWriteRight` | 7 | 0.0 % | 56.0 | da |
| 415 | `internal/service/sektorsko_izvjesce_service.go:57` · `(*IzvjescaService).SektoriZaSastavljanje` | 7 | 0.0 % | 56.0 |  |
| 416 | `internal/service/station_service.go:81` · `(*StationService).GetSectionGaugeCriteria` | 7 | 0.0 % | 56.0 | da |
| 417 | `internal/service/vodocuvar_service.go:422` · `(*VodocuvarService).Ovjeri` | 7 | 0.0 % | 56.0 |  |
| 418 | `internal/service/watercourse_service.go:97` · `(*WatercourseService).CreateWatercourse` | 7 | 0.0 % | 56.0 |  |
| 419 | `internal/service/watercourse_service.go:188` · `validateWatercourse` | 7 | 0.0 % | 56.0 |  |
| 420 | `internal/uvoz/godisnjak/godisnjak.go:123` · `podjelaStranice` | 7 | 0.0 % | 56.0 | da |
| 421 | `internal/uvoz/godisnjak/godisnjak.go:206` · `nadjiRijec` | 7 | 0.0 % | 56.0 | da |
| 422 | `internal/uvoz/his2000/posao.go:209` · `(*Posao).Krivulje` | 7 | 0.0 % | 56.0 | da |
| 423 | `internal/web/handlers_auth.go:325` · `(*AuthHandler).HandleViewAs` | 7 | 0.0 % | 56.0 | da |
| 424 | `internal/web/handlers_dbmaint.go:141` · `(*DBMaintHandler).HandleCompact` | 7 | 0.0 % | 56.0 |  |
| 425 | `internal/web/handlers_episodes.go:44` · `(*SectionsHandler).HandleDeclareDefense` | 7 | 0.0 % | 56.0 |  |
| 426 | `internal/web/handlers_journals.go:516` · `(*JournalsHandler).ShowSheet` | 7 | 0.0 % | 56.0 |  |
| 427 | `internal/web/handlers_journals.go:737` · `(*JournalsHandler).ShowPrint` | 7 | 0.0 % | 56.0 |  |
| 428 | `internal/web/handlers_maintenance.go:196` · `(*MaintenanceHandler).HandleImportUpload` | 7 | 0.0 % | 56.0 |  |
| 429 | `internal/web/handlers_modules.go:68` · `(*ModulesHandler).HandleSave` | 7 | 0.0 % | 56.0 |  |
| 430 | `internal/web/handlers_organization.go:134` · `(*OrgHandler).ShowSectorForm` | 7 | 0.0 % | 56.0 |  |
| 431 | `internal/web/handlers_organization.go:774` · `(*OrgHandler).HandleSaveTerms` | 7 | 0.0 % | 56.0 |  |
| 432 | `internal/web/handlers_paket.go:109` · `(*StationsHandler).IzveziPaket` | 7 | 0.0 % | 56.0 |  |
| 433 | `internal/web/handlers_prognoze.go:353` · `(*PrognozeHandler).SpremiPostavke` | 7 | 0.0 % | 56.0 |  |
| 434 | `internal/web/handlers_prognoze_izvoz.go:258` · `(*PrognozeHandler).zaglavljeIzvoza` | 7 | 0.0 % | 56.0 |  |
| 435 | `internal/web/handlers_prognoze_izvoz_grafovi.go:381` · `(*PrognozeHandler).godisnjeVelicine` | 7 | 0.0 % | 56.0 |  |
| 436 | `internal/web/handlers_prognoze_izvoz_grafovi.go:447` · `krajnostiIzGodina` | 7 | 0.0 % | 56.0 |  |
| 437 | `internal/web/handlers_readings.go:1033` · `(*ReadingsHandler).HandleFollow` | 7 | 0.0 % | 56.0 | da |
| 438 | `internal/web/handlers_sections_pages.go:222` · `(*SectionsHandler).imeDjelatnika` | 7 | 0.0 % | 56.0 |  |
| 439 | `internal/web/handlers_settings.go:394` · `(*SettingsHandler).HandleSetBootstrap` | 7 | 0.0 % | 56.0 |  |
| 440 | `internal/web/handlers_slivovi.go:245` · `(*SlivoviHandler).letveJSON` | 7 | 0.0 % | 56.0 |  |
| 441 | `internal/web/handlers_stations.go:467` · `(*StationsHandler).HandleDeleteStationAPI` | 7 | 0.0 % | 56.0 |  |
| 442 | `internal/web/handlers_structures.go:168` · `(*StructuresHandler).ShowStructureForm` | 7 | 0.0 % | 56.0 |  |
| 443 | `internal/web/handlers_subscriptions.go:73` · `(*SubscriptionsHandler).HandleAdd` | 7 | 0.0 % | 56.0 |  |
| 444 | `internal/web/handlers_territories.go:583` · `(*TerritoriesHandler).HandleCreateSettlementAPI` | 7 | 0.0 % | 56.0 |  |
| 445 | `internal/web/handlers_territories.go:640` · `(*TerritoriesHandler).HandleUpdateSettlementAPI` | 7 | 0.0 % | 56.0 |  |
| 446 | `internal/web/handlers_territories.go:692` · `(*TerritoriesHandler).HandleDeleteSettlementAPI` | 7 | 0.0 % | 56.0 |  |
| 447 | `internal/web/handlers_territories_pages.go:59` · `(*TerritoriesHandler).ShowCountyForm` | 7 | 0.0 % | 56.0 |  |
| 448 | `internal/web/handlers_users.go:308` · `(*UsersHandler).HandleAddDuty` | 7 | 0.0 % | 56.0 |  |
| 449 | `internal/web/handlers_users_pages.go:439` · `(*UsersHandler).moduleRows` | 7 | 0.0 % | 56.0 |  |
| 450 | `internal/web/handlers_uvoz_krivulja.go:133` · `zatecenoKrivulja` | 7 | 0.0 % | 56.0 | da |
| 451 | `internal/web/handlers_uvoz_niza.go:621` · `primiIzObrasca` | 7 | 0.0 % | 56.0 | da |
| 452 | `internal/web/handlers_uvoz_niza.go:653` · `(*UvozHandler).Zatecen` | 7 | 0.0 % | 56.0 | da |
| 453 | `internal/web/handlers_uvoz_niza.go:906` · `uzBrojLetva` | 7 | 0.0 % | 56.0 | da |
| 454 | `internal/web/handlers_vodocuvar.go:480` · `(*VodocuvarHandler).karteObuhvata` | 7 | 0.0 % | 56.0 |  |
| 455 | `internal/web/handlers_vodocuvar.go:510` · `(*VodocuvarHandler).Prilog` | 7 | 0.0 % | 56.0 |  |
| 456 | `internal/web/handlers_watercourses.go:65` · `(*WatercoursesHandler).ShowWatercourses` | 7 | 0.0 % | 56.0 |  |
| 457 | `internal/web/handlers_watercourses_pages.go:157` · `(*WatercoursesHandler).ShowWatercourseForm` | 7 | 0.0 % | 56.0 |  |
| 458 | `internal/web/karta_geo.go:72` · `paraTocka` | 7 | 0.0 % | 56.0 |  |
| 459 | `internal/web/kisa_slivova.go:114` · `najveciPorast` | 7 | 0.0 % | 56.0 |  |
| 460 | `internal/web/prognoza_citac.go:102` · `(*CitacPrognoza).Tude` | 7 | 0.0 % | 56.0 |  |
| 461 | `internal/web/server.go:1817` · `(*Server).UgradiPaket` | 7 | 0.0 % | 56.0 |  |
| 462 | `internal/web/uzduzni_jutro.go:264` · `(*PrognozeHandler).jutarnje` | 7 | 0.0 % | 56.0 |  |
| 463 | `internal/web/izvjesce.go:356` · `(IzvjesceLetve).niz` | 8 | 9.5 % | 55.4 |  |
| 464 | `internal/peers/status.go:360` · `(*Service).alerts` | 22 | 60.0 % | 53.0 | da |
| 465 | `internal/arhiva/gradnja.go:809` · `bezDalekih` | 8 | 11.8 % | 52.0 | da |
| 466 | `internal/service/sektorsko_izvjesce_service.go:189` · `(*IzvjescaService).PrijedlogTekstova` | 28 | 68.9 % | 51.7 |  |
| 467 | `internal/web/handlers_organization.go:250` · `readCSV` | 8 | 12.0 % | 51.6 |  |
| 468 | `internal/web/handlers_readings.go:772` · `(*ReadingsHandler).ShowForm` | 25 | 65.5 % | 50.8 | da |
| 469 | `internal/db/sections_link.go:246` · `watercourseIndexTx` | 10 | 26.1 % | 50.4 |  |
| 470 | `internal/web/handlers_watercourses.go:336` · `(*WatercoursesHandler).vezeLetve` | 8 | 14.3 % | 48.3 |  |
| 471 | `internal/repository/user_repo.go:750` · `(*UserRepository).dutiesForUser` | 9 | 21.4 % | 48.3 | da |
| 472 | `internal/models/models.go:263` · `(Station).Zemlja` | 24 | 66.7 % | 45.3 |  |
| 473 | `internal/xlsxw/xlsxw.go:406` · `(*List).xml` | 33 | 78.1 % | 44.4 |  |
| 474 | `internal/prognoza/baza.go:263` · `uskladi` | 13 | 43.5 % | 43.5 | da |
| 475 | `internal/arhiva/katalog.go:127` · `Izdaj` | 32 | 78.2 % | 42.6 | da |
| 476 | `internal/service/zid_service.go:214` · `(*ZidService).vidi` | 14 | 47.4 % | 42.6 |  |
| 477 | `cmd/gocop-postava/traka.go:131` · `(*traka).preuzmi` | 6 | 0.0 % | 42.0 |  |
| 478 | `cmd/gocop-postava/traka.go:179` · `(*traka).nadogradiKlik` | 6 | 0.0 % | 42.0 |  |
| 479 | `cmd/gocop-postava/traka.go:247` · `(*traka).oProgramu` | 6 | 0.0 % | 42.0 |  |
| 480 | `cmd/gocop-postava/traka.go:269` · `opisStanja` | 6 | 0.0 % | 42.0 |  |
| 481 | `cmd/gocop/arhiva_razmjena.go:367` · `imaStablo` | 6 | 0.0 % | 42.0 |  |
| 482 | `internal/db/structures_seed.go:97` · `matchStation` | 6 | 0.0 % | 42.0 |  |
| 483 | `internal/importer/bp16/bp16.go:114` · `(HTTPSource).Asset` | 6 | 0.0 % | 42.0 | da |
| 484 | `internal/importer/bp16/bp16.go:302` · `mapState` | 6 | 0.0 % | 42.0 | da |
| 485 | `internal/importer/bp16/bp16.go:318` · `mapGate` | 6 | 0.0 % | 42.0 | da |
| 486 | `internal/importer/bp16/bp16.go:639` · `(Report).Summary` | 6 | 0.0 % | 42.0 | da |
| 487 | `internal/importer/ugovor/ugovor.go:74` · `Parse` | 6 | 0.0 % | 42.0 | da |
| 488 | `internal/importer/ugovor/ugovor.go:217` · `(*Contract).parseCatalogue` | 6 | 0.0 % | 42.0 | da |
| 489 | `internal/models/akt.go:356` · `(Akt).Osnova` | 6 | 0.0 % | 42.0 |  |
| 490 | `internal/models/hidro.go:203` · `NazivVrste` | 6 | 0.0 % | 42.0 |  |
| 491 | `internal/models/hidro.go:790` · `(HQOdsjecak).Zapis` | 6 | 0.0 % | 42.0 |  |
| 492 | `internal/models/hidro.go:972` · `(HQKrivulja).NajveciSkok` | 6 | 0.0 % | 42.0 |  |
| 493 | `internal/models/izvjesce.go:174` · `(IzvjesceSadrzaj).Prazno` | 6 | 0.0 % | 42.0 |  |
| 494 | `internal/models/journal.go:267` · `RatingLabel` | 6 | 0.0 % | 42.0 |  |
| 495 | `internal/models/models.go:448` · `(DefensePhase).BadgeClass` | 6 | 0.0 % | 42.0 | da |
| 496 | `internal/models/models.go:482` · `(DefensePhase).Label` | 6 | 0.0 % | 42.0 | da |
| 497 | `internal/models/models.go:687` · `(Station).NajviseZabiljezeno` | 6 | 0.0 % | 42.0 |  |
| 498 | `internal/models/mts.go:301` · `StranaOznaka` | 6 | 0.0 % | 42.0 |  |
| 499 | `internal/models/reading.go:236` · `StructureStateLabel` | 6 | 0.0 % | 42.0 |  |
| 500 | `internal/models/roles.go:179` · `(Role).DefaultLabel` | 6 | 0.0 % | 42.0 | da |
| 501 | `internal/models/spatial.go:290` · `(Section).AllStructureIDs` | 6 | 0.0 % | 42.0 |  |
| 502 | `internal/models/spatial.go:519` · `(Section).PersonnelByLevel` | 6 | 0.0 % | 42.0 |  |
| 503 | `internal/models/user.go:203` · `(*User).PrimaryDuty` | 6 | 0.0 % | 42.0 | da |
| 504 | `internal/models/user.go:226` · `(*User).IsFieldUser` | 6 | 0.0 % | 42.0 | da |
| 505 | `internal/models/user.go:494` · `(*UserPermissions).CanAdminister` | 6 | 0.0 % | 42.0 | da |
| 506 | `internal/models/vodocuvar.go:272` · `Stavke` | 6 | 0.0 % | 42.0 |  |
| 507 | `internal/models/watercourse.go:61` · `(Watercourse).OriginLabel` | 6 | 0.0 % | 42.0 |  |
| 508 | `internal/models/watercourse.go:125` · `(Watercourse).CategoryLabel` | 6 | 0.0 % | 42.0 |  |
| 509 | `internal/models/watercourse.go:143` · `(Watercourse).Summary` | 6 | 0.0 % | 42.0 |  |
| 510 | `internal/posta/sanducic.go:475` · `Premjesti` | 6 | 0.0 % | 42.0 |  |
| 511 | `internal/postava/postava.go:163` · `(*Postava).Prati` | 6 | 0.0 % | 42.0 |  |
| 512 | `internal/postava/sustav_unix.go:86` · `ZaustaviPostavu` | 6 | 0.0 % | 42.0 |  |
| 513 | `internal/prognoza/dnevna.go:367` · `(*DnevniModel).zbrojKise` | 6 | 0.0 % | 42.0 | da |
| 514 | `internal/prognoza/dnevna.go:551` · `UlaziUPrognozu` | 6 | 0.0 % | 42.0 | da |
| 515 | `internal/prognoza/hidmet.go:116` · `DohvatiHidmet` | 6 | 0.0 % | 42.0 | da |
| 516 | `internal/prognoza/kisa_slivova.go:90` · `PragoviSlivova` | 6 | 0.0 % | 42.0 | da |
| 517 | `internal/prognoza/lanac.go:483` · `ispisiPojase` | 6 | 0.0 % | 42.0 | da |
| 518 | `internal/prognoza/namjestanje.go:360` · `kakoDrzi` | 6 | 0.0 % | 42.0 | da |
| 519 | `internal/prognoza/namjestanje.go:404` · `najboljiSam` | 6 | 0.0 % | 42.0 | da |
| 520 | `internal/prognoza/namjestanje.go:674` · `korelacija` | 6 | 0.0 % | 42.0 | da |
| 521 | `internal/prognoza/razmjena.go:181` · `PrimiOborine` | 6 | 0.0 % | 42.0 | da |
| 522 | `internal/razmjena/keys.go:106` · `LoadKey` | 6 | 0.0 % | 42.0 | da |
| 523 | `internal/repository/akti_repo.go:323` · `(*AktiRepository).DeletePrimatelj` | 6 | 0.0 % | 42.0 |  |
| 524 | `internal/repository/akti_repo.go:814` · `(*AktiRepository).DeleteZig` | 6 | 0.0 % | 42.0 |  |
| 525 | `internal/repository/arhiva_repo.go:106` · `rangIzvora` | 6 | 0.0 % | 42.0 |  |
| 526 | `internal/repository/arhiva_repo.go:818` · `(*ArhivaRepository).RedoviIzvora` | 6 | 0.0 % | 42.0 |  |
| 527 | `internal/repository/contractor_repo.go:208` · `(*OrgRepository).ContractorIndex` | 6 | 0.0 % | 42.0 |  |
| 528 | `internal/repository/drugi_korak_repo.go:194` · `(*DrugiKorakRepository).SpremiRacunalo` | 6 | 0.0 % | 42.0 |  |
| 529 | `internal/repository/journal_repo.go:377` · `(*JournalRepository).ListSheets` | 6 | 0.0 % | 42.0 |  |
| 530 | `internal/repository/journal_repo.go:588` · `(*JournalRepository).NumberGaps` | 6 | 0.0 % | 42.0 |  |
| 531 | `internal/repository/kisomjer_repo.go:139` · `(*KisomjerRepository).DeleteKisomjer` | 6 | 0.0 % | 42.0 |  |
| 532 | `internal/repository/kisomjer_repo.go:204` · `(*KisomjerRepository).DeleteSliv` | 6 | 0.0 % | 42.0 |  |
| 533 | `internal/repository/maintenance_repo.go:187` · `(*MaintenanceRepository).DeleteWater` | 6 | 0.0 % | 42.0 |  |
| 534 | `internal/repository/maintenance_repo.go:302` · `(*MaintenanceRepository).DeleteItem` | 6 | 0.0 % | 42.0 |  |
| 535 | `internal/repository/mts_repo.go:182` · `(*MtsRepository).ListVrste` | 6 | 0.0 % | 42.0 |  |
| 536 | `internal/repository/mts_repo.go:510` · `(*MtsRepository).StanjeNaTerenu` | 6 | 0.0 % | 42.0 |  |
| 537 | `internal/repository/potpis_repo.go:97` · `(*PotpisRepository).DeleteKljuc` | 6 | 0.0 % | 42.0 |  |
| 538 | `internal/repository/reading_repo.go:280` · `(*ReadingRepository).Delete` | 6 | 0.0 % | 42.0 | da |
| 539 | `internal/repository/sluzbe_repo.go:93` · `(*TerritoryRepository).DeleteSluzba` | 6 | 0.0 % | 42.0 |  |
| 540 | `internal/repository/structure_repo.go:231` · `(*StructureRepository).DeleteStructure` | 6 | 0.0 % | 42.0 |  |
| 541 | `internal/repository/territory_repo.go:347` · `(*TerritoryRepository).DeleteCounty` | 6 | 0.0 % | 42.0 |  |
| 542 | `internal/repository/user_repo.go:726` · `(*UserRepository).GetPastDutiesForUser` | 6 | 0.0 % | 42.0 | da |
| 543 | `internal/repository/vodocuvar_repo.go:228` · `(*VodocuvarRepository).Delete` | 6 | 0.0 % | 42.0 |  |
| 544 | `internal/repository/vodocuvar_repo.go:450` · `(*VodocuvarRepository).ZadaciURazdoblju` | 6 | 0.0 % | 42.0 |  |
| 545 | `internal/repository/vodocuvar_repo.go:540` · `(*VodocuvarRepository).DeleteIzvornik` | 6 | 0.0 % | 42.0 |  |
| 546 | `internal/repository/watercourse_repo.go:236` · `(*WatercourseRepository).DeleteWatercourse` | 6 | 0.0 % | 42.0 |  |
| 547 | `internal/repository/watercourse_repo.go:273` · `(*WatercourseRepository).SetStationWatercourse` | 6 | 0.0 % | 42.0 |  |
| 548 | `internal/service/akt_posta.go:1022` · `nastavakSlike` | 6 | 0.0 % | 42.0 |  |
| 549 | `internal/service/akt_service.go:257` · `(*AktService).SpremiSprancu` | 6 | 0.0 % | 42.0 | da |
| 550 | `internal/service/akt_service.go:1218` · `(*AktService).SmijeObrisatiTrajno` | 6 | 0.0 % | 42.0 | da |
| 551 | `internal/service/auth_service.go:245` · `(*AuthService).StartViewingAs` | 6 | 0.0 % | 42.0 | da |
| 552 | `internal/service/dezurstva_service.go:474` · `(*JournalService).PredajDezurstvo` | 6 | 0.0 % | 42.0 |  |
| 553 | `internal/service/izvjesca_service.go:285` · `(*IzvjescaService).DioniceZaPisanje` | 6 | 0.0 % | 42.0 |  |
| 554 | `internal/service/journal_service.go:282` · `(*JournalService).ConfirmSheet` | 6 | 0.0 % | 42.0 |  |
| 555 | `internal/service/kisomjer_service.go:38` · `(*KisomjerService).CreateKisomjer` | 6 | 0.0 % | 42.0 |  |
| 556 | `internal/service/maintenance_service.go:26` · `(*MaintenanceService).CanEdit` | 6 | 0.0 % | 42.0 |  |
| 557 | `internal/service/module_service.go:46` · `(*ModuleService).RoleMatrix` | 6 | 0.0 % | 42.0 |  |
| 558 | `internal/service/mts_service.go:928` · `(*MtsService).ObrisiVrstu` | 6 | 0.0 % | 42.0 |  |
| 559 | `internal/service/obracun_service.go:111` · `(*ObracunService).SpremiKoeficijente` | 6 | 0.0 % | 42.0 |  |
| 560 | `internal/service/obracun_service.go:126` · `oznakaIzNaziva` | 6 | 0.0 % | 42.0 |  |
| 561 | `internal/service/prijava_service.go:156` · `(*PrijavaService).nacrtVlasnika` | 6 | 0.0 % | 42.0 |  |
| 562 | `internal/service/prijava_service.go:233` · `(*PrijavaService).Slika` | 6 | 0.0 % | 42.0 |  |
| 563 | `internal/service/structure_service.go:61` · `(*StructureService).CanCreate` | 6 | 0.0 % | 42.0 |  |
| 564 | `internal/service/territory_service.go:266` · `(*TerritoryService).UpdateSettlement` | 6 | 0.0 % | 42.0 |  |
| 565 | `internal/service/vodocuvar_service.go:52` · `terenskaDuznost` | 6 | 0.0 % | 42.0 |  |
| 566 | `internal/service/zid_service.go:332` · `(*opisivac).ime` | 6 | 0.0 % | 42.0 |  |
| 567 | `internal/ulaganje/zaboravljanje.go:188` · `zaboraviUlozena` | 6 | 0.0 % | 42.0 |  |
| 568 | `internal/uvoz/godisnjak/godisnjak.go:256` · `pokupiSazetak` | 6 | 0.0 % | 42.0 | da |
| 569 | `internal/uvoz/godisnjak/godisnjak.go:357` · `daniUMjesecu` | 6 | 0.0 % | 42.0 | da |
| 570 | `internal/uvoz/hvpovijest/hvpovijest.go:174` · `PrepoznajIzvor` | 6 | 0.0 % | 42.0 | da |
| 571 | `internal/uvoz/izvori/izvori.go:191` · `uveziARSO` | 6 | 0.0 % | 42.0 | da |
| 572 | `internal/uvoz/izvori/izvori.go:327` · `uveziHIS` | 6 | 0.0 % | 42.0 | da |
| 573 | `internal/web/handlers_akti_posta.go:80` · `(*AktiHandler).HandleAdminPosta` | 6 | 0.0 % | 42.0 |  |
| 574 | `internal/web/handlers_dbmaint.go:177` · `(*DBMaintHandler).HandleVacuum` | 6 | 0.0 % | 42.0 |  |
| 575 | `internal/web/handlers_episodes.go:95` · `(*SectionsHandler).HandleEndDefense` | 6 | 0.0 % | 42.0 |  |
| 576 | `internal/web/handlers_izvori.go:88` · `(*IzvoriHandler).ShowIzvori` | 6 | 0.0 % | 42.0 |  |
| 577 | `internal/web/handlers_journals.go:561` · `(*JournalsHandler).HandleSheetConditions` | 6 | 0.0 % | 42.0 |  |
| 578 | `internal/web/handlers_journals.go:597` · `countRows` | 6 | 0.0 % | 42.0 |  |
| 579 | `internal/web/handlers_journals.go:658` · `(*JournalsHandler).HandleAddEntry` | 6 | 0.0 % | 42.0 |  |
| 580 | `internal/web/handlers_mts.go:659` · `obraneSektora` | 6 | 0.0 % | 42.0 |  |
| 581 | `internal/web/handlers_organization.go:156` · `(*OrgHandler).ShowAreaForm` | 6 | 0.0 % | 42.0 |  |
| 582 | `internal/web/handlers_pairing.go:172` · `(*PairHandler).ShowWizard` | 6 | 0.0 % | 42.0 |  |
| 583 | `internal/web/handlers_prognoze.go:381` · `(*PrognozeHandler).zaglavlje` | 6 | 0.0 % | 42.0 |  |
| 584 | `internal/web/handlers_prognoze.go:1226` · `letveProfila` | 6 | 0.0 % | 42.0 |  |
| 585 | `internal/web/handlers_prognoze_izvoz.go:24` · `(*PrognozeHandler).IzvoziPrognoze` | 6 | 0.0 % | 42.0 |  |
| 586 | `internal/web/handlers_settings.go:51` · `(*SettingsHandler).ShowSettings` | 6 | 0.0 % | 42.0 |  |
| 587 | `internal/web/handlers_stations.go:357` · `(*StationsHandler).HandleCreateStationAPI` | 6 | 0.0 % | 42.0 |  |
| 588 | `internal/web/handlers_stations_pages.go:230` · `(*StationsHandler).HistorijatLetve` | 6 | 0.0 % | 42.0 |  |
| 589 | `internal/web/handlers_structures.go:287` · `(*StructuresHandler).fillSector` | 6 | 0.0 % | 42.0 |  |
| 590 | `internal/web/handlers_subscriptions.go:141` · `(*SubscriptionsHandler).HandleRemove` | 6 | 0.0 % | 42.0 |  |
| 591 | `internal/web/handlers_tema.go:92` · `(*AktiHandler).HandleTema` | 6 | 0.0 % | 42.0 |  |
| 592 | `internal/web/handlers_territories_vode.go:84` · `(*TerritoriesHandler).vodnaGeoJSON` | 6 | 0.0 % | 42.0 |  |
| 593 | `internal/web/handlers_ulaganje.go:58` · `(*UvozHandler).PregledUlaganja` | 6 | 0.0 % | 42.0 |  |
| 594 | `internal/web/handlers_ulaganje.go:155` · `(*UvozHandler).Pospremi` | 6 | 0.0 % | 42.0 |  |
| 595 | `internal/web/handlers_users_pages.go:465` · `(*UsersHandler).HandleUserModules` | 6 | 0.0 % | 42.0 |  |
| 596 | `internal/web/handlers_uvoz_krivulja.go:98` · `pregledKrivulja` | 6 | 0.0 % | 42.0 | da |
| 597 | `internal/web/handlers_uvoz_niza.go:686` · `(*UvozHandler).PonoviPregled` | 6 | 0.0 % | 42.0 | da |
| 598 | `internal/web/pricuvno_izvoz.go:31` · `(*PrognozeHandler).IzvoziPricuvno` | 6 | 0.0 % | 42.0 |  |
| 599 | `internal/web/prijava_pdf.go:448` · `PDFPrijaveRekonstrukcija` | 6 | 0.0 % | 42.0 |  |
| 600 | `internal/web/prognoza_citac.go:187` · `(*CitacPrognoza).Izdavac` | 6 | 0.0 % | 42.0 |  |
| 601 | `internal/web/prognoze_metoda.go:702` · `(*PrognozeHandler).metoda` | 6 | 0.0 % | 42.0 |  |
| 602 | `internal/web/prognoze_sazetak.go:305` · `recenicaKise` | 6 | 0.0 % | 42.0 |  |
| 603 | `internal/web/section_veze.go:240` · `(*SectionsHandler).HandlePrijedlogVezeAPI` | 6 | 0.0 % | 42.0 |  |
| 604 | `internal/web/server.go:147` · `(*Server).JaviPostavljanje` | 6 | 0.0 % | 42.0 |  |
| 605 | `internal/web/server.go:1881` · `(*Server).IzgradiLetvu` | 6 | 0.0 % | 42.0 |  |
| 606 | `internal/xlsxw/xlsxw.go:300` · `sidroLogotipa` | 6 | 0.0 % | 42.0 |  |
| 607 | `internal/repository/section_repo.go:181` · `(*SectionRepository).SaveSectionUzProvjeru` | 20 | 62.5 % | 41.1 |  |
| 608 | `internal/service/user_service.go:650` · `(*UserService).UpdateDuty` | 28 | 74.5 % | 41.0 |  |
| 609 | `internal/xlsxw/xlsxw.go:195` · `(*Knjiga).Zapisi` | 25 | 71.8 % | 39.0 |  |
| 610 | `internal/db/sections_link.go:326` · `(*Linker).linkWater` | 7 | 13.3 % | 38.9 |  |
| 611 | `internal/web/handlers_ocitanja_csv.go:188` · `citajOcitanja` | 27 | 75.2 % | 38.1 |  |
| 612 | `internal/service/zid_service.go:680` · `(*ZidService).Stanje` | 28 | 76.9 % | 37.6 |  |
| 613 | `internal/service/sektorsko_izvjesce_service.go:372` · `(*IzvjescaService).SpremiSektorsko` | 21 | 66.7 % | 37.3 |  |
| 614 | `internal/service/sektorsko_izvjesce_service.go:31` · `(*IzvjescaService).SmijeVidjetiSektor` | 15 | 53.8 % | 37.1 |  |
| 615 | `internal/service/section_service.go:217` · `validateParts` | 18 | 61.1 % | 37.1 |  |
| 616 | `internal/web/handlers_watercourses.go:238` · `(*WatercoursesHandler).HandleUpdateWatercourseAPI` | 18 | 61.8 % | 36.0 |  |
| 617 | `internal/geometrija/enc.go:68` · `kalibrirajGeoJSON` | 28 | 79.7 % | 34.5 |  |
| 618 | `internal/web/handlers_stations.go:922` · `parseZeroDatumHistory` | 7 | 17.6 % | 34.4 |  |
| 619 | `internal/db/sections_link.go:177` · `NewLinker` | 15 | 56.0 % | 34.2 |  |
| 620 | `internal/web/handlers_imenik_exchange.go:57` · `(*AktiHandler).ShowImenik` | 19 | 65.6 % | 33.7 |  |
| 621 | `cmd/gocop/arhiva_razmjena.go:271` · `(*razmjenaArhive).stanje` | 20 | 67.6 % | 33.6 |  |
| 622 | `internal/peers/subscriptions.go:109` · `(Subscription).matches` | 14 | 53.8 % | 33.3 | da |
| 623 | `internal/repository/contractor_repo.go:108` · `(*OrgRepository).SaveContractor` | 17 | 61.9 % | 33.0 |  |
| 624 | `internal/repository/sadrzaj.go:287` · `preseliJednu` | 21 | 70.8 % | 32.0 |  |
| 625 | `internal/arhiva/gradnja.go:274` · `Izgradi` | 24 | 76.3 % | 31.7 | da |
| 626 | `internal/web/handlers_vodocuvar.go:639` · `(*VodocuvarHandler).HandleKoordinatePodrucja` | 18 | 65.5 % | 31.4 |  |
| 627 | `internal/posta/sanducic.go:294` · `ewsSirovo` | 7 | 21.4 % | 30.8 |  |
| 628 | `internal/weather/openmeteo.go:157` · `describe` | 25 | 79.3 % | 30.5 |  |
| 629 | `internal/service/izvjesca_service.go:174` · `(*IzvjescaService).Spremi` | 21 | 72.2 % | 30.5 |  |
