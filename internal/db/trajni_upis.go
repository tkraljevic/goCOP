package db

// TrajniUpis dopisuje se na kraj adrese svake baze u koju program piše, npr.
// put+"?_pragma=journal_mode(WAL)&"+TrajniUpis. Nakon nestanka struje
// potvrđen upis ne smije nestati, jer je ovjeren akt možda već poslan ili
// ispisan, a njegov broj ne smije dobiti drugi akt. Na Macu ni to ne prazni
// predmemoriju samog diska; to bi tražilo i fullfsync, koji svaku novu bazu
// u testovima usporava s 0,05 na 0,9 s.
const TrajniUpis = "_pragma=synchronous(FULL)"
