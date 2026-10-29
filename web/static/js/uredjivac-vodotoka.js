// Uređivanje toka vode na karti (Leaflet-Geoman): crte toka se crtaju,
// pomiču i brišu, a spremanje šalje crte na /api/watercourses/geometry, gdje
// ih program provjeri kao i učitani GeoJSON. Šalju se samo crte: točke
// stacionaže (rkm) program uzme iz spremljenog zapisa, jer karta Dunava i
// Drave prikazuje stacionažu izračunatu prema ENC-u, a ne spremljenu.
// Karta je ona koju je app.js nacrtao za .karta-vodotoka (okvir._karta,
// okvir._tok); natpisi i gumbi su u uredjivac-karte.js.
(function () {
  function jeCrta(f) {
    var t = f && f.geometry && f.geometry.type;
    return t === 'LineString' || t === 'MultiLineString';
  }

  function pokreni(okvir) {
    window.uredjivacKarte(okvir, {
      sloj: okvir._tok,
      adresa: '/api/watercourses/geometry',
      stil: okvir._stilToka,
      kontrole: {
        position: 'topleft',
        drawMarker: false, drawPolyline: true, drawPolygon: false,
        drawCircle: false, drawCircleMarker: false, drawRectangle: false, drawText: false,
        editMode: true, dragMode: true, cutPolygon: false, removalMode: true, rotateMode: false
      },
      naslovi: {
        drawLineButton: 'Nova crta toka',
        dragButton: 'Pomakni cijeli dio toka',
        deleteButton: 'Obriši dio toka'
      },
      nova: function (sloj) {
        sloj.feature = sloj.feature || { type: 'Feature', properties: { tip: 'vodotok' } };
      },
      tijelo: function (gj) {
        var crte = (gj.features || []).filter(jeCrta);
        if (crte.length === 0) {
          alert('Tok ne može ostati prazan. Nacrtajte barem jednu crtu toka ili pritisnite Odustani.');
          return null;
        }
        var zbirka = { type: 'FeatureCollection', features: crte };
        return { code: okvir.dataset.sifra, geojson: JSON.stringify(zbirka), samo_crte: true };
      }
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    if (!window.uredjivacKarte) return;
    document.querySelectorAll('.karta-vodotoka').forEach(pokreni);
  });
})();
