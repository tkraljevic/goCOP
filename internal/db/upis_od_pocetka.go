package db

// UpisOdPocetka dopisuje se, uz TrajniUpis, na kraj adrese svake baze u koju
// program piše. Transakcija (BeginTx) tada pravo pisanja traži odmah na
// početku (BEGIN IMMEDIATE), a ne tek kod prvog upisa.
//
// Obična transakcija koja je najprije čitala, a onda htjela pisati, padala je
// s „database is locked” bez čekanja kad je u isto vrijeme pisao netko drugi:
// SQLite tada ne čeka busy_timeout, jer bi upis išao preko stanja koje
// transakcija nije vidjela. Tako je čvor gubio bilješke razmjene (laptop
// 2. 10. 2026.), a isto je prijetilo uvozu očitanja, primjeni razmjene i
// spremanju s weba. Ovako druga transakcija na početku pričeka da prva
// završi. Transakcija sa sql.TxOptions{ReadOnly: true} ostaje obična i
// nikoga ne čeka.
const UpisOdPocetka = "_txlock=immediate"
