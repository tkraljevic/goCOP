// Odziv na klik: čovjek mora odmah vidjeti da je klik primljen, inače
// pritisne opet i obrazac ode dvaput (Odluke (3. 11. 2026.)). Pritisnuti gumb
// istog trena dobije vrtuljak, a obrazac se do odgovora ne šalje ponovno.
// Traka pri vrhu javi se tek kad učitavanje potraje, da brze stranice ne
// trepere; kad odgovor dugo ne stiže, kaže to riječima. Loadera preko
// cijelog zaslona nema: kad na terenu padne veza, ne smije zarobiti stranicu.
//
// Obrazac ili poveznica koja samo preuzima datoteku (Excel, PDF, CSV)
// ostavlja stranicu gdje jest, pa se kod njih gumb odblokira sam. Takvu
// adresu bez nastavka označava data-preuzimanje. Obrasci i poveznice koje
// skripta sama obrađuje (preventDefault) ovdje se ne diraju.
(function () {
  var TRAKA_NAKON = 200;      // ms do trake pri vrhu
  var SPORO_NAKON = 15000;    // ms do poruke da traje dulje
  var ODUSTANI_NAKON = 45000; // ms do odblokiranja gumba
  var PREUZIMANJE = 3000;     // ms koliko gumb preuzimanja ostaje zauzet
  var NASTAVAK_DATOTEKE = /\.(xlsx|pdf|csv|zip|cop|geojson|kml|gpx|txt|pem|png|jpe?g)$/i;
  var BRISANJE = /\/(delete|obrisi|brisi|brisanje|ukloni|makni|storno|ponisti|opozovi)(\/|$)/i;

  // poruka kad odgovor kasni kaže što čovjek vidi, ne što program radi
  // iznutra; brisanje i storno su obrada, ne spremanje
  var SPORO = {
    ucitavanje: 'Učitavanje traje dulje nego obično…',
    spremanje: 'Spremanje traje dulje nego obično…',
    obrada: 'Obrada traje dulje nego obično…'
  };

  var traka = null, poruka = null, satovi = [];

  function element(oznaka, razred) {
    var e = document.createElement(oznaka);
    e.className = razred;
    document.body.appendChild(e);
    return e;
  }

  function ocisti() {
    satovi.forEach(clearTimeout);
    satovi = [];
    if (traka) traka.classList.remove('aktivna');
    if (poruka) poruka.hidden = true;
  }

  function javi(tekst) {
    if (!poruka) {
      poruka = element('div', 'odziv-poruka');
      poruka.setAttribute('role', 'status');
    }
    poruka.textContent = tekst;
    poruka.hidden = false;
  }

  // pocni pokazuje traku pri vrhu i, kad odgovor kasni, poruku; kraj se
  // zove kad se zna da stranica ostaje (preuzimanje, odustajanje)
  function pocni(vrsta, trajanje, kraj) {
    ocisti();
    if (!traka) traka = element('div', 'odziv-traka');
    satovi.push(setTimeout(function () { traka.classList.add('aktivna'); }, TRAKA_NAKON));
    if (trajanje) {
      satovi.push(setTimeout(function () { ocisti(); if (kraj) kraj(); }, trajanje));
      return;
    }
    satovi.push(setTimeout(function () {
      javi(SPORO[vrsta] || SPORO.ucitavanje);
    }, SPORO_NAKON));
    satovi.push(setTimeout(function () {
      if (kraj) kraj();
      ocisti();
      javi('Odgovor nije stigao. Provjerite vezu i pokušajte ponovno.');
      satovi.push(setTimeout(function () { poruka.hidden = true; }, 8000));
    }, ODUSTANI_NAKON));
  }

  // vrstaSlanja: obrazac koji samo traži učitava, brisanje je obrada, a
  // ostalo spremanje
  function vrstaSlanja(nacin, adresa) {
    if (nacin.toLowerCase() !== 'post') return 'ucitavanje';
    try {
      if (BRISANJE.test(new URL(adresa, location.href).pathname)) return 'obrada';
    } catch (e) {}
    return 'spremanje';
  }

  function preuzima(el, adresa) {
    if (el.hasAttribute('download') || el.hasAttribute('data-preuzimanje')) return true;
    try {
      return NASTAVAK_DATOTEKE.test(new URL(adresa, location.href).pathname);
    } catch (e) {
      return false;
    }
  }

  // gumbi obrasca ostaju aktivni za preglednik (onemogućen gumb ne bi
  // poslao svoje ime i vrijednost), ali izgledaju zauzeto i ne primaju klik
  function zauzmi(obrazac, pritisnut) {
    obrazac.dataset.odzivSalje = '1';
    obrazac.querySelectorAll('button[type="submit"], button:not([type]), input[type="submit"]').forEach(function (g) {
      g.setAttribute('aria-disabled', 'true');
      g.classList.add('odziv-ceka');
    });
    if (pritisnut) {
      pritisnut.classList.add('odziv-salje');
      pritisnut.setAttribute('aria-busy', 'true');
    }
  }

  function oslobodi(obrazac) {
    delete obrazac.dataset.odzivSalje;
    obrazac.querySelectorAll('.odziv-ceka, .odziv-salje').forEach(function (g) {
      g.removeAttribute('aria-disabled');
      g.removeAttribute('aria-busy');
      g.classList.remove('odziv-ceka', 'odziv-salje');
    });
  }

  // na prozoru, da skripte na stranici prve odluče hoće li obrazac otići
  window.addEventListener('submit', function (e) {
    var obrazac = e.target;
    if (!(obrazac instanceof HTMLFormElement)) return;
    if (obrazac.dataset.odzivSalje) { e.preventDefault(); return; }
    if (e.defaultPrevented) return;
    var pritisnut = e.submitter || null;
    // obrazac dijaloga samo zatvara prozor, stranica ostaje
    var nacin = (pritisnut && pritisnut.getAttribute('formmethod')) || obrazac.getAttribute('method') || '';
    if (nacin.toLowerCase() === 'dialog') return;
    var cilj = (pritisnut && pritisnut.getAttribute('formtarget')) || obrazac.getAttribute('target') || '';
    if (cilj && cilj !== '_self') return;
    var adresa = (pritisnut && pritisnut.getAttribute('formaction')) || obrazac.getAttribute('action') || location.href;
    zauzmi(obrazac, pritisnut);
    var datoteka = preuzima(obrazac, adresa) || (pritisnut && pritisnut.hasAttribute('data-preuzimanje'));
    pocni(vrstaSlanja(nacin, adresa), datoteka ? PREUZIMANJE : 0, function () { oslobodi(obrazac); });
  });

  // poveznica na drugu stranicu: samo traka, bez blokiranja
  window.addEventListener('click', function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest && e.target.closest('a[href]');
    if (!a) return;
    var cilj = a.getAttribute('target');
    if (cilj && cilj !== '_self') return;
    var href = a.getAttribute('href');
    if (!href || href.charAt(0) === '#' || /^(mailto|tel|javascript|blob|data):/i.test(href)) return;
    var url;
    try { url = new URL(href, location.href); } catch (err) { return; }
    if (url.origin !== location.origin) return;
    // samo druga oznaka na istoj stranici
    if (url.pathname === location.pathname && url.search === location.search && url.hash) return;
    pocni('ucitavanje', preuzima(a, href) ? PREUZIMANJE : 0);
  });

  // form.submit() iz skripte (filtar koji se šalje čim se promijeni izbor) ne
  // javlja događaj submit, pa ga se dočeka ovdje. Samo traka, bez zauzimanja
  // obrasca: izbor se smije odmah ispraviti.
  var posalji = HTMLFormElement.prototype.submit;
  HTMLFormElement.prototype.submit = function () {
    var cilj = this.getAttribute('target') || '';
    if (!cilj || cilj === '_self') {
      var adresa = this.getAttribute('action') || location.href;
      pocni(vrstaSlanja(this.getAttribute('method') || '', adresa), preuzima(this, adresa) ? PREUZIMANJE : 0);
    }
    return posalji.call(this);
  };

  // gumb koji je upravo pritisnut, za radnje koje skripta šalje sama (fetch)
  var zadnji = null, zadnjiKad = 0;
  window.addEventListener('click', function (e) {
    var g = e.target.closest && e.target.closest('button, input[type="button"], input[type="submit"]');
    if (g) { zadnji = g; zadnjiKad = Date.now(); }
  }, true);

  // cekaj pokazuje da radnja čeka odgovor: gumb (kad ga ima) vrti i ne prima
  // klik, traka je pri vrhu, a nakon 15 s piše da traje dulje. Ne odustaje
  // sama, jer razmjena zna trajati i minutama. Vraća gotovo(), koje sve
  // vrati kako je bilo.
  function cekaj(gumb, natpis) {
    var izvorno = null, gotov = false;
    ocisti();
    if (!traka) traka = element('div', 'odziv-traka');
    satovi.push(setTimeout(function () { traka.classList.add('aktivna'); }, TRAKA_NAKON));
    satovi.push(setTimeout(function () { javi(SPORO.obrada); }, SPORO_NAKON));
    if (gumb) {
      gumb.classList.add('odziv-ceka', 'odziv-salje');
      gumb.setAttribute('aria-busy', 'true');
      gumb.setAttribute('aria-disabled', 'true');
      if (natpis) { izvorno = gumb.innerHTML; gumb.textContent = natpis; }
    }
    return function () {
      if (gotov) return;
      gotov = true;
      ocisti();
      if (!gumb) return;
      gumb.classList.remove('odziv-ceka', 'odziv-salje');
      gumb.removeAttribute('aria-busy');
      gumb.removeAttribute('aria-disabled');
      if (izvorno !== null) gumb.innerHTML = izvorno;
    };
  }

  // Za skripte: odziv.idi(adresa) i odziv.ponovno() otvaraju stranicu uz
  // traku; odziv.gumb(natpis) čeka na gumbu pritisnutom u zadnje dvije
  // sekunde, a odziv.traka() samo s trakom (sloj karte, popis).
  window.odziv = {
    idi: function (adresa) { pocni('ucitavanje'); location.href = adresa; },
    ponovno: function () { pocni('ucitavanje'); location.reload(); },
    gumb: function (natpis) { return cekaj(zadnji && Date.now() - zadnjiKad < 2000 ? zadnji : null, natpis); },
    traka: function () { return cekaj(null); }
  };

  // povratak gumbom Natrag vraća stranicu iz predmemorije s gumbima kakvi su
  // ostali, pa ih treba osloboditi
  window.addEventListener('pageshow', function (e) {
    if (!e.persisted) return;
    ocisti();
    document.querySelectorAll('form[data-odziv-salje]').forEach(oslobodi);
  });
})();
