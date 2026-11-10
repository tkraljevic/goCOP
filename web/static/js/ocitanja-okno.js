// Popis Očitanja i letva jedno uz drugo (goCOP-m3, 12 · Očitanja): na
// širokom zaslonu stranice letve (Kartica, Očitanja, Historijat) dobiju popis
// lijevo, s otvorenom letvom istaknutom. Filtri popisa pamte se za karticu
// preglednika, pa popis uz letvu i povratak na popis zadrže isti sektor,
// područje i pretragu. Na uskom zaslonu popisa uz letvu nema.
(function () {
  var KLJUC = 'gocop.ocitanja.filtri';
  var SIROKO = '(min-width: 1200px)';

  function zapamti(qs) { try { sessionStorage.setItem(KLJUC, qs); } catch (e) { /* bez pamćenja */ } }
  function filtri() { try { return sessionStorage.getItem(KLJUC) || ''; } catch (e) { return ''; } }
  function bezStranice(qs) {
    var p = new URLSearchParams(qs);
    p.delete('page'); p.delete('okno'); p.delete('odabrana');
    return p.toString();
  }

  // pretraga se otvara povećalom, izbornik čipa zatvara klikom izvan i Escom
  // (isto na stranici i u popisu uz letvu, pa slušamo na dokumentu)
  document.addEventListener('click', function (e) {
    var b = e.target.closest('[data-otvori-trazi]');
    if (b) {
      var f = document.getElementById(b.getAttribute('aria-controls')), otvori = f.hidden;
      f.hidden = !otvori;
      b.setAttribute('aria-expanded', otvori ? 'true' : 'false');
      if (otvori) f.querySelector('input[type=search]').focus();
    }
    document.querySelectorAll('.ocitanja-izbor[open]').forEach(function (d) { if (!d.contains(e.target)) d.open = false; });
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') document.querySelectorAll('.ocitanja-izbor[open]').forEach(function (d) { d.open = false; });
  });

  function pokreni() {
    if (location.pathname === '/readings') zapamti(bezStranice(location.search.replace(/^\?/, '')));
    var okno = document.querySelector('[data-okno-popisa]');
    if (!okno) return;
    var siroko = window.matchMedia(SIROKO);

    // ucitaj puni popis; rukom (čip, pretraga) uz traku i poruku kad ne uspije
    function ucitaj(qs, rukom) {
      var gotovo = rukom ? window.odziv.traka() : function () {};
      var p = new URLSearchParams(qs);
      p.set('okno', '1');
      p.set('odabrana', okno.dataset.oknoPopisa);
      return fetch('/readings?' + p.toString(), { credentials: 'same-origin' }).then(function (r) {
        if (!r.ok) throw new Error(r.status);
        return r.text();
      }).then(function (html) {
        okno.innerHTML = html;
        // pomiče se samo popis: stranica koju osoba već čita ostaje gdje jest
        var odabrana = okno.querySelector('[aria-current="page"]');
        if (odabrana) {
          okno.scrollTop += odabrana.getBoundingClientRect().top - okno.getBoundingClientRect().top -
            (okno.clientHeight - odabrana.offsetHeight) / 2;
        }
      }).catch(function () {
        // bez popisa letva radi kao i prije; već učitan popis ostaje
        if (!okno.firstElementChild) okno.hidden = true;
        else if (rukom && window.showToast) window.showToast('Popis nije osvježen. Provjerite vezu i pokušajte ponovno.');
      }).then(gotovo);
    }

    // čipovi i pretraga u popisu uz letvu mijenjaju samo popis, letva ostaje
    okno.addEventListener('click', function (e) {
      var a = e.target.closest('.ocitanja-cipovi a, .ocitanja-izbornik a, .ocitanja-trazi a, .ocitanja-podnozje a');
      if (!a) return;
      e.preventDefault();
      var qs = bezStranice((a.getAttribute('href').split('?')[1]) || '');
      zapamti(qs);
      ucitaj(qs, true);
    });
    okno.addEventListener('submit', function (e) {
      var f = e.target.closest('form.ocitanja-trazi');
      if (!f) return;
      e.preventDefault();
      var qs = bezStranice(new URLSearchParams(new FormData(f)).toString());
      zapamti(qs);
      ucitaj(qs, true);
    });

    function provjeri() {
      if (siroko.matches && !okno.dataset.ucitano) {
        okno.dataset.ucitano = '1';
        ucitaj(filtri());
      }
    }
    provjeri();
    if (siroko.addEventListener) siroko.addEventListener('change', provjeri);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', pokreni);
  else pokreni();
})();
