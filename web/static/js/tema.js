// Tema stranice: učitava se u <head> prije stilova, da stranica ne bljesne.
// Tema je uvijek zapisana na <html data-theme="…">, pa CSS tamnu temu
// opisuje samo jednom ([data-theme="dark"]). Izbor iz zaglavlja (sunce/mjesec)
// pamti se u pregledniku; bez izbora tema slijedi sustav, i kad se on
// promijeni dok je stranica otvorena. Ispis je uvijek u svijetloj temi.
(function () {
  var h = document.documentElement;
  // razred js kaže stilu da skripta radi; bez nje izbornik stoji kao običan popis
  h.classList.add('js');
  var sustav = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;

  function izbor() {
    try {
      var t = localStorage.getItem('gocop-theme');
      if (t === 'dark' || t === 'light') return t;
    } catch (e) {}
    return null;
  }

  function postavi() {
    h.setAttribute('data-theme', izbor() || (sustav && sustav.matches ? 'dark' : 'light'));
  }

  postavi();

  if (sustav) {
    var promjena = function () { if (!izbor()) postavi(); };
    if (sustav.addEventListener) sustav.addEventListener('change', promjena);
    else if (sustav.addListener) sustav.addListener(promjena);
  }


  // Izbornik: sužen (uska traka) kad ga je korisnik suzio ili je ekran uži
  // od 840 px; na mobitelu (ispod 600 px) stoji donja traka, pa se ne sužava.
  // Od 840 px izbornik stoji u mjestu, pa otvoreni preko sadržaja ne ostaje
  // kad se prozor raširi. Gumbi koji ga otvaraju (aria-controls) kažu je li
  // raširen.
  function izbornik() {
    var suzen = false;
    try { suzen = localStorage.getItem('gocop-izbornik') === 'suzen'; } catch (e) {}
    var w = window.innerWidth;
    var body = document.body;
    if (w >= 840 && body) body.classList.remove('izbornik-otvoren');
    var otvoren = !!body && body.classList.contains('izbornik-otvoren');
    var uski = !otvoren && w >= 600 && (suzen || w < 840);
    h.classList.toggle('izbornik-uski', uski);
    var gumbi = document.querySelectorAll('[aria-controls="izbornik"]');
    for (var i = 0; i < gumbi.length; i++) {
      gumbi[i].setAttribute('aria-expanded', otvoren || (w >= 840 && !uski) ? 'true' : 'false');
    }
  }
  izbornik();
  document.addEventListener('DOMContentLoaded', izbornik);
  window.addEventListener('resize', izbornik);
  // prijelaz preko granice širine javlja se i kad resize stigne prije nove širine
  if (window.matchMedia) {
    ['(min-width: 600px)', '(min-width: 840px)'].forEach(function (upit) {
      var mq = window.matchMedia(upit);
      if (mq.addEventListener) mq.addEventListener('change', izbornik);
      else if (mq.addListener) mq.addListener(izbornik);
    });
  }
  window.izbornikOsvjezi = izbornik;

  window.addEventListener('beforeprint', function () { h.setAttribute('data-theme', 'light'); });
  window.addEventListener('afterprint', postavi);
})();
