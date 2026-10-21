// Paket mrezatest daje veze i slušalice u memoriji, za testove razmjene bez
// mreže i bez portova. Ime je po uzoru na httptest i iotest.
//
// net.Pipe za to nije dovoljan: pisanje na njemu čeka da druga strana
// pročita. Zato zapne svaki razgovor u kojem obje strane pišu u isto vrijeme,
// npr. kad obje strane odmah nakon TLS rukovanja pošalju svoju prvu poruku,
// ili kad TLS 1.3 poslužitelj šalje kartu sesije dok klijent već piše.
// Takav test stoji do roka (provjereno 21. 10. 2026.). Ovdje svaki smjer veze
// ima međuspremnik od 64 KiB, kao TCP, pa pisanje ne čeka čitatelja dok ima
// mjesta.
//
// Objekti koji se koriste u mjehuru (testing/synctest) nastaju u njemu. Tada
// kašnjenje i rokovi idu po probnom satu mjehura, a čekanje na podatke
// synctest vidi kao trajno blokirano. Izvan mjehura sve radi u stvarnom
// vremenu.
//
// Što nije oponašano:
//   - RST pri zatvaranju veze s nepročitanim podacima. Druga strana pročita
//     sve što joj je poslano i zatim dobije io.EOF, a ne prekid veze. Testovi
//     odbijanja i zauzetosti zato ostaju na loopbacku.
//   - Pisanje prema strani koja je zatvorila vezu vraća broken pipe čim
//     zatvaranje stigne do pisača: na vezi s kašnjenjem jedno kašnjenje
//     nakon zatvaranja, a bez kašnjenja odmah. Na TCP-u prvo takvo pisanje
//     obično još prođe, a greška dođe tek s RST-om. Zato npr. okvir za kraj
//     WebSocketa ili TLS veze, poslan kao odgovor strani koja je već
//     zatvorila, vraća grešku.
//   - Trajanje uspostave veze. Kašnjenje vrijedi za podatke i za zatvaranje,
//     a Nazovi i DialContext vraćaju vezu bez kašnjenja (čekaju samo kad je
//     zaostatak slušalice pun).
package mrezatest

// Adresa je net.Addr zadane mreže i teksta, npr. TCP adresa s izmišljenim
// IP-om koju test sam odabere
type Adresa struct{ Mreza, Tekst string }

// TCP je TCP adresa zadana tekstom, npr. "192.0.2.7:4710"
func TCP(tekst string) Adresa { return Adresa{Mreza: "tcp", Tekst: tekst} }

// Network vraća ime mreže
func (a Adresa) Network() string { return a.Mreza }

// String vraća adresu kao tekst
func (a Adresa) String() string { return a.Tekst }
