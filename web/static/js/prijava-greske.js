// Prijava greške ili prijedloga s ikone kukca u gornjoj traci. Skripta se
// učitava prva, da zapamti i greške drugih skripti na stranici. Ništa ovdje ne
// smije smetati radu: svaka greška prijave ostaje u obrascu, a stranica se
// ne mijenja. Šalje se samo što je osoba upisala i podaci o stranici, nikad
// sadržaj stranice, lozinke ni kolačići. Sliku zaslona osoba bira sama.
(function () {
  'use strict';

  var NAJVISE_GRESAKA = 5;
  var ROK_SLANJA_MS = 20000;
  var NAJVISE_NASLOVA = 120;
  var NAJVISE_ODABRANOG = 40;
  // slika na sporoj vezi s terena putuje dulje; granica je ista kao na poslužitelju
  var ROK_SLANJA_SLIKE_MS = 180000;
  var NAJVECA_SLIKA = 2 * 1024 * 1024;
  var VRSTE_SLIKE = ['image/png', 'image/jpeg', 'image/webp'];
  var greske = [];
  var izvorniFetch = window.fetch ? window.fetch.bind(window) : null;

  function dvije(n) { return (n < 10 ? '0' : '') + n; }

  function zapamti(poruka) {
    try {
      var tekst = String(poruka || '').replace(/\?[^\s"')]*/g, '').slice(0, 200);
      if (!tekst) return;
      var d = new Date();
      greske.push(dvije(d.getHours()) + ':' + dvije(d.getMinutes()) + ':' + dvije(d.getSeconds()) + ' ' + tekst);
      if (greske.length > NAJVISE_GRESAKA) greske.shift();
    } catch (e) { /* bilježenje ne smije izazvati novu grešku */ }
  }

  function samoPut(adresa) {
    try { return new URL(adresa, location.href).pathname; } catch (e) { return ''; }
  }

  // Namjerno prekinut poziv (listač koji krene dalje) nije greška stranice
  function jePrekid(razlog) { return !!razlog && razlog.name === 'AbortError'; }

  window.addEventListener('error', function (e) {
    var t = e.target;
    if (t && t !== window && t.tagName) {
      // slike i pločice karte znaju izostati i bez greške programa
      if (t.tagName === 'SCRIPT' || t.tagName === 'LINK') zapamti('Nije se učitalo: ' + samoPut(t.src || t.href));
      return;
    }
    zapamti((e.message || 'Greška skripte') + (e.filename ? ' (' + samoPut(e.filename) + ':' + e.lineno + ')' : ''));
  }, true);

  window.addEventListener('unhandledrejection', function (e) {
    var r = e.reason;
    if (jePrekid(r)) return;
    zapamti('Nedovršen posao: ' + (r && r.message ? r.message : r));
  });

  // greške poslužitelja: samo status i put, bez upita i bez odgovora
  if (izvorniFetch) {
    window.fetch = function (ulaz) {
      var obecanje = izvorniFetch.apply(null, arguments);
      try {
        var put = samoPut(ulaz && ulaz.url ? ulaz.url : ulaz);
        obecanje.then(function (r) {
          if (r.status >= 500) zapamti('Poslužitelj je javio grešku ' + r.status + ' za ' + put);
        }, function (razlog) {
          if (!jePrekid(razlog)) zapamti('Veza s poslužiteljem nije uspjela za ' + put);
        });
      } catch (e) { /* bilježenje ne smije smetati pozivu */ }
      return obecanje;
    };
  }

  // Naziv stranice: naslov u traci, naslov sadržaja i što je odabrano
  // (kartica, čip). Samo oznake kojima se osoba kreće, ne sadržaj.
  var ODABRANO = 'main [aria-current="page"], main [aria-current="true"], main [aria-selected="true"], main [aria-pressed="true"], main [data-odabran], main .ploca-kartica.aktivna, main .posta-mapa.active';

  // Na stranici jednog zapisa naslov je sadržaj: predmet pisma, ime osobe,
  // predmet akta. Zato se čita samo na općim stranicama, bez oznake zapisa u
  // adresi i bez upita, ili kad ga predložak sam označi. Oznaka s vrijednošću
  // kaže vrstu stranice („Novo pismo”), a bez vrijednosti znači da naslov nije
  // osoban (ime letve, dionice, objekta).
  function naslovJeOpci() {
    var put = location.pathname;
    return !location.search && !/\d/.test(put) && !/^\/(profile|posta|vodocuvar)/.test(put);
  }

  function nazivStranice() {
    var dijelovi = [];
    function dodaj(t, najvise) {
      t = (t || '').replace(/\s+/g, ' ').trim();
      if (!t || /^[\d\s]+$/.test(t) || t.length > najvise || dijelovi.indexOf(t) >= 0) return;
      dijelovi.push(t);
    }
    var traka = document.querySelector('.vrh-naslov');
    if (traka) dodaj(traka.textContent, NAJVISE_NASLOVA);
    var oznaceni = document.querySelector('main [data-naziv-stranice]');
    if (oznaceni) dodaj(oznaceni.getAttribute('data-naziv-stranice') || oznaceni.textContent, NAJVISE_NASLOVA);
    else if (naslovJeOpci()) {
      var naslov = document.querySelector('main h1');
      if (naslov) dodaj(naslov.textContent, NAJVISE_NASLOVA);
    }
    // Odabrano što se ne vidi (skriveni pogled Karte) osoba nije ni otvorila.
    // Odabrani redak popisa letvi nosi i vrijednosti, a ovdje treba samo ime.
    document.querySelectorAll(ODABRANO).forEach(function (el) {
      if (dijelovi.length >= 5 || !el.getClientRects().length) return;
      dodaj((el.querySelector('.ocitanja-ime') || el).textContent, NAJVISE_ODABRANOG);
    });
    return dijelovi.join(' · ') || location.pathname;
  }

  function pokreni() {
    var okno = document.getElementById('prijava-greske');
    var gumb = document.getElementById('vrh-prijava-greske');
    if (!okno || !gumb || !okno.showModal) return;
    var obrazac = okno.querySelector('form');
    var opis = okno.querySelector('[name=opis]');
    var natpisOpisa = okno.querySelector('label[for="pg-opis"]');
    var poruka = okno.querySelector('.pg-poruka');
    var posalji = okno.querySelector('.pg-posalji');
    var dioObrasca = okno.querySelector('.pg-dio-obrazac');
    var dioPotvrde = okno.querySelector('.pg-dio-potvrda');
    var ulazSlike = okno.querySelector('.pg-slika-ulaz');
    var dodajSliku = okno.querySelector('.pg-slika-dodaj');
    var dodanaSlika = okno.querySelector('.pg-slika-dodana');
    var malaSlika = okno.querySelector('.pg-slika-mala');
    var imeSlike = okno.querySelector('.pg-slika-ime');
    var naziv = '';
    var adresaSlike = '';

    function javi(tekst) {
      poruka.textContent = tekst;
      poruka.hidden = !tekst;
    }

    // sličica živi samo u pregledniku, a poslužitelj sliku dobije tek slanjem
    function makniSliku() {
      if (adresaSlike) URL.revokeObjectURL(adresaSlike);
      adresaSlike = '';
      ulazSlike.value = '';
      malaSlika.removeAttribute('src');
      imeSlike.textContent = '';
      dodanaSlika.hidden = true;
      dodajSliku.hidden = false;
    }

    function razlogOdbijanjaSlike(datoteka) {
      if (datoteka.type && VRSTE_SLIKE.indexOf(datoteka.type) < 0) return 'Slika mora biti PNG, JPEG ili WebP. Odaberi drugu datoteku.';
      if (datoteka.size > NAJVECA_SLIKA) return 'Slika je prevelika. Najviše je 2 MB. Odaberi manju.';
      if (!datoteka.size) return 'Datoteka je prazna. Odaberi drugu.';
      return '';
    }

    ulazSlike.addEventListener('change', function () {
      var datoteka = ulazSlike.files[0];
      if (!datoteka) { makniSliku(); return; }
      var razlog = razlogOdbijanjaSlike(datoteka);
      if (razlog) {
        makniSliku();
        javi(razlog);
        return;
      }
      javi('');
      if (adresaSlike) URL.revokeObjectURL(adresaSlike);
      adresaSlike = URL.createObjectURL(datoteka);
      malaSlika.src = adresaSlike;
      imeSlike.textContent = datoteka.name;
      dodajSliku.hidden = true;
      dodanaSlika.hidden = false;
      okno.querySelector('.pg-slika-ukloni').focus();
    });

    okno.querySelector('.pg-slika-ukloni').addEventListener('click', function () {
      makniSliku();
      javi('');
      ulazSlike.focus();
    });

    function prikazi(potvrda) {
      dioObrasca.hidden = potvrda;
      dioPotvrde.hidden = !potvrda;
    }

    // Nova prijava je greška, a izbor ostaje dok se okno samo zatvori bez slanja
    function natpis() {
      if (!okno.querySelector('[name=vrsta]:checked')) okno.querySelector('[name=vrsta][value=greska]').checked = true;
      var prijedlog = okno.querySelector('[name=vrsta]:checked').value === 'prijedlog';
      natpisOpisa.textContent = prijedlog ? 'Što ti se ne sviđa i što predlažeš?' : 'Što se dogodilo?';
    }

    gumb.addEventListener('click', function () {
      naziv = nazivStranice();
      okno.querySelector('.pg-stranica-naziv').textContent = naziv;
      javi('');
      prikazi(false);
      natpis();
      okno.showModal();
      opis.focus();
    });

    okno.addEventListener('change', function (e) {
      if (e.target.name === 'vrsta') natpis();
    });
    okno.querySelector('.pg-odustani').addEventListener('click', function () { okno.close(); });
    okno.querySelector('.pg-zatvori').addEventListener('click', function () { okno.close(); });
    okno.addEventListener('close', function () { gumb.focus(); });

    function uspjeh(d) {
      obrazac.reset();
      makniSliku();
      okno.querySelector('.pg-potvrda-stranica').textContent = d.stranica || naziv;
      okno.querySelector('.pg-potvrda-vrijeme').textContent = (d.vrsta || '') + ' · ' + (d.kad || '');
      prikazi(true);
      okno.querySelector('.pg-zatvori').focus();
    }

    // Odjavljen korisnik i račun s privremenom lozinkom dobiju preusmjeravanje
    // na drugu stranicu umjesto odgovora. Sve ovo je neuspjelo slanje, a ne
    // greška koja prekida rad.
    function procitaj(r) {
      var vrsta = r.headers.get('Content-Type') || '';
      if (vrsta.indexOf('application/json') >= 0) return r.json();
      if (r.status === 413) return { success: false, error: 'Slika je prevelika. Najviše je 2 MB. Odaberi manju ili je ukloni pa pošalji.' };
      if (r.status === 403) return r.text().then(function (t) { return { success: false, error: t.slice(0, 300) }; });
      if (r.redirected || r.status === 401) return { success: false, error: 'Prijava nije poslana. Možda si odjavljen ili moraš promijeniti lozinku: otvori program u novoj kartici pa pošalji ponovno. Tekst je ostao ovdje.' };
      return { success: false, error: 'Prijava nije poslana. Tekst je ostao ovdje, pa pokušaj ponovno za koji trenutak.' };
    }

    obrazac.addEventListener('submit', function (e) {
      e.preventDefault();
      if (!opis.value.trim()) {
        javi('Upiši što želiš prijaviti.');
        opis.focus();
        return;
      }
      var podaci = new FormData(obrazac);
      var saSlikom = ulazSlike.files.length > 0;
      if (!saSlikom) podaci.delete('slika');
      podaci.set('stranica', location.pathname + location.search);
      podaci.set('naziv_stranice', naziv);
      podaci.set('zaslon', window.innerWidth + ' × ' + window.innerHeight);
      greske.forEach(function (g) { podaci.append('greske', g); });
      javi('');
      posalji.disabled = true;
      var prekid = window.AbortController ? new AbortController() : null;
      var rok = prekid ? setTimeout(function () { prekid.abort(); }, saSlikom ? ROK_SLANJA_SLIKE_MS : ROK_SLANJA_MS) : 0;
      izvorniFetch('/prijava-greske', {
        method: 'POST', body: podaci, credentials: 'same-origin',
        headers: { Accept: 'application/json' }, signal: prekid ? prekid.signal : undefined
      }).then(procitaj).then(function (d) {
        if (d && d.success) uspjeh(d);
        else javi((d && d.error) || 'Prijava nije poslana. Tekst je ostao ovdje, pa pokušaj ponovno.');
      }).catch(function () {
        javi('Veza s poslužiteljem je prekinuta, pa ne znamo je li prijava stigla. Tekst je ostao ovdje. Možeš je poslati ponovno jer se ista prijava neće spremiti dvaput, ili zatvoriti i nastaviti s radom.');
      }).then(function () {
        clearTimeout(rok);
        posalji.disabled = false;
      });
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', pokreni);
  else pokreni();
})();
