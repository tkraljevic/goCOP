// Stranica prijave: oko uz lozinku pokazuje i skriva upisano. Bez skripte
// polje ostaje obična lozinka, a gumb nema što raditi, pa je skriven dok
// skripta ne stigne.
(function () {
  var gumb = document.querySelector('.prijava-oko');
  var polje = document.getElementById('password');
  if (!gumb || !polje) return;
  gumb.hidden = false;
  gumb.addEventListener('click', function () {
    var pokazi = polje.type === 'password';
    polje.type = pokazi ? 'text' : 'password';
    gumb.setAttribute('aria-pressed', pokazi ? 'true' : 'false');
    gumb.setAttribute('aria-label', pokazi ? 'Sakrij lozinku' : 'Pokaži lozinku');
  });
})();
