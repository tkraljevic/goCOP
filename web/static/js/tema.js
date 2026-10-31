// Tema stranice: učitava se u <head> prije stilova, da stranica ne bljesne.
// Tema je uvijek zapisana na <html data-theme="…">, pa CSS tamnu temu
// opisuje samo jednom ([data-theme="dark"]). Izbor iz zaglavlja (sunce/mjesec)
// pamti se u pregledniku; bez izbora tema slijedi sustav, i kad se on
// promijeni dok je stranica otvorena. Ispis je uvijek u svijetloj temi.
(function () {
  var h = document.documentElement;
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
  // od 840 px; na mobitelu (ispod 600 px) stoji donja traka, pa se ne sužava
  function izbornik() {
    var suzen = false;
    try { suzen = localStorage.getItem('gocop-izbornik') === 'suzen'; } catch (e) {}
    var w = window.innerWidth;
    var otvoren = document.body && document.body.classList.contains('izbornik-otvoren');
    h.classList.toggle('izbornik-uski', !otvoren && w >= 600 && (suzen || w < 840));
  }
  izbornik();
  window.addEventListener('resize', izbornik);
  window.izbornikOsvjezi = izbornik;

  window.addEventListener('beforeprint', function () { h.setAttribute('data-theme', 'light'); });
  window.addEventListener('afterprint', postavi);
})();
