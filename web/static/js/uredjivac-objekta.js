// Uređivanje objekta na karti (Leaflet-Geoman): točke, crte i poligoni se
// dodaju, pomiču i brišu, a spremanje šalje cijeli objekt na
// /api/structures/geometry, gdje ga program provjeri kao i učitani GeoJSON.
// Karta je ona koju je app.js nacrtao za .karta-objekta (okvir._karta,
// okvir._objekt); natpisi i gumbi su u uredjivac-karte.js.
(function () {
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
    var vrsta = okvir.dataset.vrsta || '';
    window.uredjivacKarte(okvir, {
      sloj: okvir._objekt,
      adresa: '/api/structures/geometry',
      kontrole: {
        position: 'topleft',
        drawMarker: true, drawPolyline: true, drawPolygon: true,
        drawCircle: false, drawCircleMarker: false, drawRectangle: false, drawText: false,
        editMode: true, dragMode: true, cutPolygon: false, removalMode: true, rotateMode: false
      },
      nova: function (sloj) {
        var f = sloj.feature || { type: 'Feature', properties: {} };
        f.properties = svojstvaNove(vrsta, sloj);
        sloj.feature = f;
        if (f.properties.km) sloj.bindTooltip('km ' + f.properties.km, { direction: 'top', offset: [0, -4] });
      },
      tijelo: function (gj) {
        var prazno = !gj.features || gj.features.length === 0;
        if (prazno && !window.confirm('Na karti nije ostalo ništa. Obrisati crtu objekta? Prethodna ostaje u povijesti zapisa.')) return null;
        return { id: okvir.dataset.id, geojson: prazno ? '' : JSON.stringify(gj) };
      }
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    if (!window.uredjivacKarte) return;
    document.querySelectorAll('.karta-objekta').forEach(pokreni);
  });
})();
