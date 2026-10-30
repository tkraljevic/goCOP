package db

// TrajniUpis stoji na kraju adrese svake baze u koju program piše; takvu
// adresu slaže ZaPisanje. Potvrđen upis ne smije nestati ni kad se računalo
// sruši ili nestane struje, jer je ovjeren akt možda već poslan ili ispisan,
// a njegov broj ne smije dobiti drugi akt.
//
// Na Macu to vrijedi samo za pad sustava. Ondje fsync ne prazni predmemoriju
// samog diska, pa nestanak struje ili prisilno gašenje i dalje mogu odnijeti
// zadnje upise, a bazu i pokvariti. Kvar baze spriječio bi već
// checkpoint_fullfsync(1), a gubitak upisa tek fullfsync(1). Oboje čeka
// odluku: s prvim su testovi trećinu do pola sporiji, a s drugim i sedam puta.
const TrajniUpis = "_pragma=synchronous(FULL)"
