// Mjera uz pokazivač dok se na karti crta ili pomiče vrh. Geoman Free sam
// ne mjeri, pa duljinu crte i površinu poligona računa ovaj dio. Duljina do
// 1 km piše se u metrima, iznad toga u kilometrima; površina do 1 ha u m²,
// iznad toga u hektarima. Uključuje je uredjivac-karte.js kad se pritisne Uredi.
(function () {
  var R = 6371000; // polumjer Zemlje u metrima, isti kao u Leafletovu distance

  function broj(n, dec) {
    return Number(n).toLocaleString('hr-HR', { minimumFractionDigits: dec, maximumFractionDigits: dec });
  }

  // udaljenost dviju točaka po kugli ({lat, lng}, u metrima)
  function udaljenost(a, b) {
    var r = Math.PI / 180, f1 = a.lat * r, f2 = b.lat * r;
    var h = Math.pow(Math.sin((f2 - f1) / 2), 2) + Math.cos(f1) * Math.cos(f2) * Math.pow(Math.sin((b.lng - a.lng) * r / 2), 2);
    return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
  }

  function duljina(tocke) {
    var d = 0;
    for (var i = 1; i < tocke.length; i++) d += udaljenost(tocke[i - 1], tocke[i]);
    return d;
  }

  function opseg(tocke) {
    return tocke.length < 2 ? 0 : duljina(tocke) + udaljenost(tocke[tocke.length - 1], tocke[0]);
  }

  // površina poligona na kugli, u m² (isti račun kao geodesicArea u Leaflet.draw)
  function povrsina(tocke) {
    if (tocke.length < 3) return 0;
    var r = Math.PI / 180, a = 0;
    for (var i = 0; i < tocke.length; i++) {
      var p1 = tocke[i], p2 = tocke[(i + 1) % tocke.length];
      a += (p2.lng - p1.lng) * r * (2 + Math.sin(p1.lat * r) + Math.sin(p2.lat * r));
    }
    return Math.abs(a * R * R / 2);
  }

  function metri(m) {
    return m < 1000 ? broj(Math.round(m), 0) + ' m' : broj(m / 1000, 2) + ' km';
  }

  function hektari(m2) {
    return m2 < 10000 ? broj(Math.round(m2), 0) + ' m²' : broj(m2 / 10000, 2) + ' ha';
  }

  function tekstPovrsine(prsten) {
    return hektari(povrsina(prsten)) + ' · opseg ' + metri(opseg(prsten));
  }

  // dok se crta: postavljene točke i pokazivač. Crta piše ukupnu duljinu i
  // dio od zadnje točke do pokazivača, poligon površinu i opseg.
  function tekstCrtanja(tocke, kursor, poligon) {
    if (!tocke.length) return '';
    var sve = tocke.concat([kursor]);
    if (poligon && sve.length >= 3) return tekstPovrsine(sve);
    var dio = udaljenost(tocke[tocke.length - 1], kursor);
    return tocke.length === 1 ? metri(dio) : metri(duljina(sve)) + ' (+' + metri(dio) + ')';
  }

  // nacrtani dio: crta ili više crta zbroje se, poligon (vanjski prsten) daje površinu
  function tekstSloja(sloj) {
    var t = sloj.getLatLngs ? sloj.getLatLngs() : [];
    if (typeof L !== 'undefined' && sloj instanceof L.Polygon) {
      var prsten = Array.isArray(t[0]) ? t[0] : t;
      if (Array.isArray(prsten[0])) prsten = prsten[0];
      return tekstPovrsine(prsten);
    }
    var dijelovi = Array.isArray(t[0]) ? t : [t];
    return metri(dijelovi.reduce(function (z, d) { return z + duljina(d); }, 0));
  }

  function oznaka(karta) {
    var el = L.DomUtil.create('div', 'karta-mjera', karta.getContainer());
    el.hidden = true;
    var skrij = null;
    return {
      pokazi: function (tekst, ll) {
        clearTimeout(skrij);
        if (!tekst || !ll) { el.hidden = true; return; }
        var p = karta.latLngToContainerPoint(ll);
        el.textContent = tekst;
        el.style.left = p.x + 'px';
        el.style.top = p.y + 'px';
        el.hidden = false;
      },
      // nakon pomicanja vrha mjera ostane kratko vidljiva, da se pročita
      sakrij: function (kasnije) {
        clearTimeout(skrij);
        if (kasnije) skrij = setTimeout(function () { el.hidden = true; }, kasnije);
        else el.hidden = true;
      }
    };
  }

  // prati pali mjeru na karti: dok se crta crta ili poligon, i dok se
  // pomiče vrh nekog dijela u sloju koji se uređuje
  function prati(karta, sloj) {
    var o = oznaka(karta);
    karta.on('pm:drawstart', function (e) {
      if (e.shape !== 'Line' && e.shape !== 'Polygon') return;
      var radni = e.workingLayer, crtac = karta.pm.Draw[e.shape];
      // pokazivač je Geomanova oznaka koja se lijepi na točke, pa i dio do
      // pokazivača odgovara mjestu na koje će pasti sljedeća točka
      var kursor = crtac && crtac._hintMarker;
      function osvjezi() {
        var ll = kursor && kursor.getLatLng();
        if (ll) o.pokazi(tekstCrtanja(radni.getLatLngs(), ll, e.shape === 'Polygon'), ll);
      }
      if (kursor) kursor.on('move', osvjezi);
      radni.on('pm:vertexadded', osvjezi);
      karta.once('pm:drawend', function () {
        if (kursor) kursor.off('move', osvjezi);
        o.sakrij();
      });
    });
    sloj.on('pm:markerdrag', function (e) {
      var vrh = e.markerEvent && e.markerEvent.target;
      if (e.layer && vrh && vrh.getLatLng) o.pokazi(tekstSloja(e.layer), vrh.getLatLng());
    });
    sloj.on('pm:markerdragend', function () { o.sakrij(2000); });
  }

  var mjera = { udaljenost: udaljenost, duljina: duljina, opseg: opseg, povrsina: povrsina, metri: metri, hektari: hektari, tekstCrtanja: tekstCrtanja, prati: prati };
  if (typeof window !== 'undefined') window.mjeraKarte = mjera;
  if (typeof module !== 'undefined') module.exports = mjera;
})();
