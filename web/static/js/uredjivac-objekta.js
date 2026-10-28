// Uređivanje objekta na karti (Leaflet-Geoman): točke, crte i poligoni se
// dodaju, pomiču i brišu, a spremanje šalje cijeli objekt na
// /api/structures/geometry, gdje ga program provjeri kao i učitani GeoJSON.
// Karta je ona koju je app.js nacrtao za .karta-objekta (okvir._karta,
// okvir._objekt).
(function () {
  var HR = {
    tooltips: {
      placeMarker: 'Kliknite za novu točku',
      firstVertex: 'Kliknite za prvu točku',
      continueLine: 'Kliknite za sljedeću točku',
      finishLine: 'Kliknite zadnju točku ili pritisnite Završi',
      finishPoly: 'Kliknite prvu točku da zatvorite poligon',
      finishRect: 'Kliknite za kraj',
      startCircle: 'Kliknite za središte',
      finishCircle: 'Kliknite za polumjer',
      placeCircleMarker: 'Kliknite za točku',
      placeText: 'Kliknite za tekst'
    },
    actions: {
      finish: 'Završi',
      cancel: 'Odustani',
      removeLastVertex: 'Makni zadnju točku'
    },
    buttonTitles: {
      drawMarkerButton: 'Nova točka',
      drawPolyButton: 'Novi poligon',
      drawLineButton: 'Nova crta',
      drawCircleButton: 'Krug',
      drawRectButton: 'Pravokutnik',
      editButton: 'Pomiči točke i vrhove (desni klik briše vrh)',
      dragButton: 'Pomakni cijeli dio objekta',
      cutButton: 'Izreži dio poligona',
      deleteButton: 'Obriši dio objekta',
      drawCircleMarkerButton: 'Kružna oznaka',
      snappingButton: 'Prianjanje',
      pinningButton: 'Spoji vrhove',
      rotateButton: 'Okreni',
      drawTextButton: 'Tekst'
    }
  };

  // za nasip i branu točka je stacionaža: pita se km, prazno je obična točka
  function svojstvaNove(vrsta, sloj) {
    if (sloj instanceof L.Marker) {
      if (vrsta === 'NASIP' || vrsta === 'BRANA') {
        var km = (window.prompt('Km stacionaže za ovu točku (npr. 1+250). Ostavite prazno za običnu točku.', '') || '').trim();
        if (km) return { tip: 'km', km: km };
      }
      return { tip: 'tocka' };
    }
    if (sloj instanceof L.Polygon) return { tip: 'poligon' };
    return { tip: vrsta === 'NASIP' ? 'nasip' : 'crta' };
  }

  function pokreni(okvir) {
    var sekcija = okvir.closest('.detail-section');
    var gumbUredi = sekcija && sekcija.querySelector('.karta-uredi-btn');
    var gumbSpremi = sekcija && sekcija.querySelector('.karta-spremi-btn');
    var gumbOdustani = sekcija && sekcija.querySelector('.karta-odustani-btn');
    var karta = okvir._karta, objekt = okvir._objekt;
    if (!gumbUredi || !karta || !objekt || !karta.pm) return;
    var vrsta = okvir.dataset.vrsta || '';

    karta.pm.setLang('hr', HR, 'en');
    karta.pm.setGlobalOptions({ layerGroup: objekt, snappable: true, snapDistance: 15 });

    gumbUredi.addEventListener('click', function () {
      gumbUredi.hidden = true;
      gumbSpremi.hidden = false;
      gumbOdustani.hidden = false;
      okvir.classList.add('u-uredjivanju');
      karta.pm.addControls({
        position: 'topleft',
        drawMarker: true, drawPolyline: true, drawPolygon: true,
        drawCircle: false, drawCircleMarker: false, drawRectangle: false, drawText: false,
        editMode: true, dragMode: true, cutPolygon: false, removalMode: true, rotateMode: false
      });
    });

    karta.on('pm:create', function (e) {
      var f = e.layer.feature || { type: 'Feature', properties: {} };
      f.properties = svojstvaNove(vrsta, e.layer);
      e.layer.feature = f;
      if (f.properties.km) e.layer.bindTooltip('km ' + f.properties.km, { direction: 'top', offset: [0, -4] });
    });

    gumbOdustani.addEventListener('click', function () { location.reload(); });

    gumbSpremi.addEventListener('click', function () {
      var gj = objekt.toGeoJSON();
      var prazno = !gj.features || gj.features.length === 0;
      if (prazno && !window.confirm('Na karti nije ostalo ništa. Obrisati crtu objekta? Prethodna ostaje u povijesti zapisa.')) return;
      gumbSpremi.disabled = true;
      fetch('/api/structures/geometry', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: okvir.dataset.id, geojson: prazno ? '' : JSON.stringify(gj) })
      }).then(function (res) {
        if (!res.ok) return res.text().then(function (t) { gumbSpremi.disabled = false; alert('Nije spremljeno: ' + t.trim()); });
        location.hash = 'karta';
        location.reload();
      }).catch(function () { gumbSpremi.disabled = false; alert('Nije spremljeno: nema veze s programom.'); });
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    document.querySelectorAll('.karta-objekta').forEach(pokreni);
  });
})();
