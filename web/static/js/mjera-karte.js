// Mjera uz pokazivač dok se na karti crta ili pomiče vrh. Geoman Free sam
// ne mjeri, pa duljinu crte i površinu poligona ovaj dio piše uz pokazivač;
// računica je u mjera.js. Uključuje je uređivač na Karti (karta-uredjivanje.js).
(function () {
  // računica je zajednička s ravnalom (mjera.js)
  var M = window.mjera;
  var udaljenost = M.udaljenost, duljina = M.duljina, opseg = M.opseg, povrsina = M.povrsina, metri = M.metri, hektari = M.hektari;

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
  // pomiče vrh nekog dijela u sloju koji se uređuje. Karta uređuje više
  // objekata zaredom, pa oznaka i praćenje crtanja idu na kartu jednom.
  function prati(karta, sloj) {
    var o = karta._mjeraKarte;
    if (!o) {
      o = karta._mjeraKarte = oznaka(karta);
      pratiCrtanje(karta, o);
    }
    sloj.on('pm:markerdrag', function (e) {
      var vrh = e.markerEvent && e.markerEvent.target;
      if (e.layer && vrh && vrh.getLatLng) o.pokazi(tekstSloja(e.layer), vrh.getLatLng());
    });
    sloj.on('pm:markerdragend', function () { o.sakrij(2000); });
  }

  function pratiCrtanje(karta, o) {
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
  }

  var mjera = { udaljenost: udaljenost, duljina: duljina, opseg: opseg, povrsina: povrsina, metri: metri, hektari: hektari, tekstCrtanja: tekstCrtanja, prati: prati };
  if (typeof window !== 'undefined') window.mjeraKarte = mjera;
})();
