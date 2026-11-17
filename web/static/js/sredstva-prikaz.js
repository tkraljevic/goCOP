// Sredstva: čipovi iznad tablice („ima / sve vrste”, „samo razlike”)
// postave data-prikaz na okvir tablice, a CSS sakrije retke bez sadržaja.
// Naslov skupine ostaje samo kad ispod njega ima vidljivih redaka.
(function () {
  function skupine(okvir) {
    okvir.querySelectorAll('[data-skupina]').forEach(function (g) {
      var prazna = true;
      for (var r = g.nextElementSibling; r && !r.hasAttribute('data-skupina'); r = r.nextElementSibling) {
        if (getComputedStyle(r).display !== 'none') { prazna = false; break; }
      }
      g.classList.toggle('sr-skupina-prazna', prazna);
    });
  }
  document.querySelectorAll('.sr-prikaz[data-cilj]').forEach(function (cipovi) {
    var okvir = document.getElementById(cipovi.getAttribute('data-cilj'));
    if (!okvir) return;
    cipovi.querySelectorAll('button[data-prikaz]').forEach(function (b) {
      b.addEventListener('click', function () {
        okvir.setAttribute('data-prikaz', b.getAttribute('data-prikaz'));
        cipovi.querySelectorAll('button[data-prikaz]').forEach(function (o) {
          o.setAttribute('aria-pressed', o === b ? 'true' : 'false');
        });
        skupine(okvir);
      });
    });
    skupine(okvir);
  });
})();
