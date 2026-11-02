// Naslovna: kartice čije je računanje sporo (vrijeme, vodostaji) najprije
// imaju samo glavu, a tijelo se učita nakon stranice. Kad učitavanje ne
// uspije, kartica to kaže umjesto da ostane na „Učitavam…”.
(function () {
  if (!window.fetch) return;
  document.querySelectorAll('.kartica-naslovne[data-lijeno]').forEach(function (k) {
    var tijelo = k.querySelector('.kartica-tijelo');
    fetch(k.getAttribute('data-lijeno'), {credentials: 'same-origin'}).then(function (r) {
      if (r.status !== 200) throw new Error(String(r.status));
      return r.text();
    }).then(function (t) {
      var okvir = document.createElement('div');
      okvir.innerHTML = t;
      var nova = okvir.querySelector('.kartica-naslovne');
      if (nova) { k.replaceWith(nova); } else if (tijelo) { tijelo.innerHTML = t; }
    }).catch(function () {
      if (tijelo) tijelo.innerHTML = '<p class="kartica-prazna nedostupno">Podaci trenutačno nisu dostupni. Osvježite stranicu za koju minutu.</p>';
    });
  });
})();
