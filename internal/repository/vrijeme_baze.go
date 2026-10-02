package repository

import "time"

// Vremena koja se upisuju u bazu.
//
// modernc.org/sqlite time.Time piše kao t.String() ("2006-01-02 15:04:05
// -0700 MST") i pri čitanju ga raščlanjuje istim oblikom. Zonu bez imena
// ispiše kao "+0200 +0200", a to se više ne da pročitati: Scan u time.Time
// javi "unsupported Scan, storing driver.Value type string into type
// *time.Time". Takvu zonu Go daje kad iz JSON-a pročita pomak koji u tom
// trenutku ne vrijedi u mjesnoj zoni: "+02:00" na čvoru bez TZ-a (UTC), ali
// i "+02:00" za siječanj u Zagrebu. Verzije iz knjige i razmjene stižu kao
// JSON, pa vrijeme iz zapisa prije upisa prolazi kroz jedan od pomoćnika
// ispod, prema tome kako stupac čuva vrijeme:
//
//   - stupci u UTC-u (created_at, updated_at i ostali žigovi koje lokalni
//     upis puni iz time.Now().UTC()) dobivaju .UTC() ili nullTime, uvijek, ne
//     samo kad zona nema ime: stupac tako ostaje u jednom obliku, a ORDER BY
//     i usporedbe teksta (MAX(last_login_at), expires_at > CURRENT_TIMESTAMP)
//     rade po trenutku;
//   - stupci u mjesnom vremenu (dani i trenuci koje čovjek upisuje u
//     Zagrebu) dobivaju vrijemeZaBazu ili vrijemeZaBazuP: zona s imenom
//     ostaje kakva jest, zona bez imena prelazi u mjesnu.
//
// Globalno rješenje je odbačeno. DSN _time_format=sqlite piše drukčiji oblik
// ("…05-07:00") od onog u postojećim recima, pa bi isti stupac imao dva
// oblika i usporedbe teksta bi griješile, osim uz pretvorbu svih redaka
// svih tablica. DSN _timezone svako vrijeme prije upisa prebaci u zadanu
// zonu (i UTC stupce) i vremena bez zone pri čitanju tumači u njoj, pa bi
// CURRENT_TIMESTAMP i stupci u UTC-u počeli odstupati. Omotač upravljača
// (NamedValueChecker) koji zonu bez imena prebacuje u mjesnu ne bi mijenjao
// postojeće retke, ali bi za UTC stupce birao mjesno vrijeme mimo stupca,
// pokrivao bi samo baze otvorene kroz db.OpenDB, a test i alat koji
// otvaraju "sqlite" izravno ostali bi bez njega. Ovdje svaki stupac vidljivo
// kaže u kojem obliku čuva vrijeme, a TestRazmjenaSvihEntitetaUZoniBezImena
// pada kad neki upis ostane bez pomoćnika.

// vrijemeZaBazu vraća trenutak u zoni koja se iz baze da pročitati: zona s
// imenom ostaje, zona bez imena prelazi u mjesnu (time.Local), a ako ni ta
// nema ime, u UTC
func vrijemeZaBazu(t time.Time) time.Time {
	if ime, _ := t.Zone(); ime != "" {
		return t
	}
	l := t.In(time.Local)
	if ime, _ := l.Zone(); ime != "" {
		return l
	}
	return t.UTC()
}

// vrijemeZaBazuP je vrijemeZaBazu za vrijeme koje smije izostati; nil je NULL
func vrijemeZaBazuP(t *time.Time) any {
	if t == nil {
		return nil
	}
	return vrijemeZaBazu(*t)
}

// nullTime je .UTC() za vrijeme koje smije izostati; nil je NULL
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
