// Dnevnik COP-a: stariji dani su sklopljeni (<details>). Kad adresa vodi na
// zapis ili dan (#zapis-12, #dan-2026-10-26, npr. s Naslovne ili nakon
// ispravka u prijepisu), otvaraju se svi sklopljeni blokovi oko njega, pa se
// stiže na sam zapis, ne na zatvoren dan. Promjena dana u unosu piše se u
// sažetak „dan: …”.
(function () {
  "use strict";

  function otvori() {
    var id = decodeURIComponent(location.hash.slice(1));
    if (!id) return;
    var cilj = document.getElementById(id);
    if (!cilj) return;
    var sklopljen = false;
    for (var el = cilj.parentElement; el; el = el.parentElement) {
      if (el.tagName === "DETAILS" && !el.open) {
        el.open = true;
        sklopljen = true;
      }
    }
    if (sklopljen) cilj.scrollIntoView();
  }

  function danUnosa() {
    var polje = document.getElementById("z-date");
    var natpis = document.querySelector(".unos-dan-vrijednost");
    if (!polje || !natpis) return;
    var danas = polje.defaultValue;
    polje.addEventListener("change", function () {
      var d = polje.value.split("-");
      natpis.textContent = !polje.value || polje.value === danas ? "danas" : (+d[2]) + ". " + (+d[1]) + ". " + d[0] + ".";
    });
  }

  window.addEventListener("hashchange", otvori);
  document.addEventListener("DOMContentLoaded", function () {
    otvori();
    danUnosa();
  });
})();
