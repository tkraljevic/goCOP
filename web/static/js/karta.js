// Karta: sve ucrtano na jednoj karti, sa slojevima koji se pale i gase
// (karta.html). Crte i točke dohvaća s data-podaci, granice županija s
// data-zupanije; boje i ikone vrsta su iz Administracije › Karte
// (postavkeKarte u app.js). Adresa pamti položaj, podlogu i isključene
// slojeve: #poz=45.6,18.7,10&podloga=Satelit&bez=letve,zupanije
(function () {
  var ZADANO = { oko: [45.55, 18.7], zum: 9 };
  var USKI = '(max-width: 839px)';

  function boja(vrsta) {
    var stil = (postavkeKarte().objekti || {})[vrsta] || {};
    return stil.boja || (getComputedStyle(document.documentElement).getPropertyValue('--primary') || '').trim() || '#173e74';
  }

  function bojaRijeke() {
    return (getComputedStyle(document.documentElement).getPropertyValue('--karta-rijeka') || '').trim() || '#0284c7';
  }

  function prozor(p) {
    return '<strong>' + escapeHtml(p.naziv || '') + '</strong>' +
      (p.adresa ? '<br><a href="' + escapeHtml(p.adresa) + '">Otvori stranicu</a>' : '');
  }

  // stanje iz adrese: položaj, podloga i isključeni slojevi
  function procitajAdresu() {
    var p = new URLSearchParams(location.hash.replace(/^#/, ''));
    var poz = (p.get('poz') || '').split(',').map(parseFloat);
    return {
      poz: poz.length === 3 && poz.every(isFinite) ? poz : null,
      podloga: p.get('podloga') || '',
      bez: (p.get('bez') || '').split(',').filter(Boolean)
    };
  }

  function zapisiAdresu(karta, okvir, podloga) {
    var c = karta.getCenter(), p = new URLSearchParams();
    p.set('poz', c.lat.toFixed(5) + ',' + c.lng.toFixed(5) + ',' + karta.getZoom());
    if (podloga) p.set('podloga', podloga);
    var bez = [];
    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) { if (!s.checked) bez.push(s.dataset.sloj); });
    if (bez.length) p.set('bez', bez.join(','));
    history.replaceState(null, '', '#' + p.toString().replace(/%2C/g, ','));
  }

  // objekti po slojevima: nasip crtom boje svoje vrste, ostali ikonom vrste
  function slojeviObjekata(zbirka) {
    var poSlojevima = {};
    (zbirka.features || []).forEach(function (f) {
      var k = f.properties.sloj;
      (poSlojevima[k] = poSlojevima[k] || []).push(f);
    });
    var out = {};
    Object.keys(poSlojevima).forEach(function (k) {
      out[k] = L.geoJSON({ type: 'FeatureCollection', features: poSlojevima[k] }, {
        style: function (f) {
          var stil = (postavkeKarte().objekti || {})[f.properties.vrsta] || {};
          return { color: boja(f.properties.vrsta), weight: stil.debljina || 4, opacity: 0.9 };
        },
        pointToLayer: function (f, ll) {
          return tockaObjekta(ll, (postavkeKarte().objekti || {})[f.properties.vrsta] || {}, boja(f.properties.vrsta));
        },
        onEachFeature: function (f, l) { l.bindPopup(prozor(f.properties)); }
      });
    });
    return out;
  }

  // letve su male točke: na karti cijelog sektora ih je stotinjak, a
  // stupić letve iz ostalih karata ih pretrpa
  function slojLetvi(letve) {
    var grupa = L.featureGroup();
    var b = boja('LETVA');
    letve.forEach(function (l) {
      L.circleMarker([l.lat, l.lon], { radius: 5, color: '#ffffff', weight: 1.5, fillColor: b, fillOpacity: 1 })
        .bindTooltip(escapeHtml(l.naziv), { direction: 'top', offset: [0, -4] }).bindPopup(prozor(l)).addTo(grupa);
    });
    return grupa;
  }

  function slojVodotoka(zbirka) {
    return L.geoJSON(zbirka, {
      style: function () { return { color: bojaRijeke(), weight: 3, opacity: 0.85, className: 'rijeka-linija' }; },
      onEachFeature: function (f, l) { l.bindPopup(prozor(f.properties)); }
    });
  }

  function slojZupanija(zbirka) {
    return L.geoJSON(zbirka, {
      style: function () { return { color: '#8e909a', weight: 1.5, dashArray: '6 4', fill: false, interactive: false }; }
    });
  }

  // prekidač skupine je uključen kad su svi njezini slojevi, a napola kad su neki
  function osvjeziSkupine(okvir) {
    okvir.querySelectorAll('.karta-skupina').forEach(function (s) {
      var slojevi = Array.prototype.slice.call(s.querySelectorAll('input[data-sloj]'));
      var ukljuceno = slojevi.filter(function (x) { return x.checked; }).length;
      var sve = s.querySelector('[data-skupina-prekidac]');
      sve.checked = ukljuceno > 0 && ukljuceno === slojevi.length;
      sve.indeterminate = ukljuceno > 0 && ukljuceno < slojevi.length;
    });
  }

  function primijeniSlojeve(karta, okvir, slojevi) {
    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) {
      var sloj = slojevi[s.dataset.sloj];
      if (!sloj) return;
      if (s.checked) sloj.addTo(karta); else karta.removeLayer(sloj);
    });
    osvjeziSkupine(okvir);
  }

  // čipovi podloge: obična karta, karta bez interneta i podloge iz postavki
  function cipoviPodloga(karta, okvir, osnovni, odabrana, promjena) {
    var podloge = podlogeKarte(osnovni), mjesto = okvir.querySelector('.karta-podloge');
    var trenutna = podloge[odabrana] ? odabrana : 'Karta';
    Object.keys(podloge).forEach(function (naziv) {
      var c = document.createElement('button');
      c.type = 'button';
      c.className = 'karta-cip';
      c.textContent = naziv;
      c.setAttribute('aria-pressed', naziv === trenutna ? 'true' : 'false');
      c.addEventListener('click', function () {
        karta.removeLayer(podloge[trenutna]);
        trenutna = naziv;
        podloge[trenutna].addTo(karta);
        mjesto.querySelectorAll('.karta-cip').forEach(function (x) { x.setAttribute('aria-pressed', x === c ? 'true' : 'false'); });
        promjena(trenutna === 'Karta' ? '' : trenutna);
      });
      mjesto.appendChild(c);
    });
    podloge[trenutna].addTo(karta);
    return trenutna === 'Karta' ? '' : trenutna;
  }

  function plocaSlojeva(okvir, karta) {
    var gumb = okvir.querySelector('.karta-slojevi-gumb');
    function otvori(da) {
      okvir.classList.toggle('karta-slojevi-zatvoreno', !da);
      gumb.setAttribute('aria-expanded', da ? 'true' : 'false');
    }
    otvori(!window.matchMedia(USKI).matches);
    gumb.addEventListener('click', function () { otvori(true); });
    okvir.querySelector('.karta-slojevi-zatvori').addEventListener('click', function () { otvori(false); });
    okvir.querySelectorAll('.karta-skupina-otvori').forEach(function (b) {
      b.addEventListener('click', function () {
        var otvoreno = b.getAttribute('aria-expanded') !== 'true';
        b.setAttribute('aria-expanded', otvoreno ? 'true' : 'false');
        document.getElementById(b.getAttribute('aria-controls')).hidden = !otvoreno;
      });
    });
    // znak crte u boji vrste (nasip) ili vode
    okvir.querySelectorAll('.karta-sloj-crta').forEach(function (z) {
      z.style.setProperty('--karta-boja', z.dataset.vrsta ? boja(z.dataset.vrsta) : bojaRijeke());
    });
    return karta;
  }

  // ikone se smanjuju kad se karta odzumira, da se na pregledu sektora ne
  // preklapaju; od 13. zuma su pune veličine
  function mjeriloIkona(zum) {
    return zum >= 13 ? 1 : zum >= 11 ? 0.8 : zum >= 9 ? 0.6 : 0.45;
  }

  function smanjiIkone(karta, okvir, slojevi) {
    var m = mjeriloIkona(karta.getZoom());
    okvir.style.setProperty('--karta-ikona', m);
    if (slojevi.letve) slojevi.letve.eachLayer(function (l) { if (l.setRadius) l.setRadius(Math.max(2.5, 5 * m)); });
  }

  function pocetniPogled(karta, stanje, slojevi) {
    if (stanje.poz) { karta.setView([stanje.poz[0], stanje.poz[1]], stanje.poz[2]); return; }
    var granice = L.latLngBounds([]);
    Object.keys(slojevi).forEach(function (k) {
      if (k !== 'vodotoci' && k !== 'letve' && k !== 'zupanije' && slojevi[k].getBounds) granice.extend(slojevi[k].getBounds());
    });
    if (!granice.isValid() && slojevi.letve) granice = slojevi.letve.getBounds();
    if (!granice.isValid()) { karta.setView(ZADANO.oko, ZADANO.zum); return; }
    var desno = window.matchMedia(USKI).matches ? 0 : 384;
    karta.fitBounds(granice, { paddingTopLeft: [24, 24], paddingBottomRight: [desno + 24, 24], maxZoom: 13 });
  }

  function pokreni(okvir) {
    var platno = okvir.querySelector('.karta-platno');
    var stanje = procitajAdresu();
    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) { if (stanje.bez.indexOf(s.dataset.sloj) >= 0) s.checked = false; });
    var karta = L.map(platno, { scrollWheelZoom: true, zoomControl: true });
    karta.setView(ZADANO.oko, ZADANO.zum);
    dodajKontroleKarte(karta, platno, { fullscreenTarget: okvir });
    plocaSlojeva(okvir, karta);

    var podloga = '';
    function zapisi() { zapisiAdresu(karta, okvir, podloga); }
    if (okvir.dataset.plocice) {
      var osnovni = L.tileLayer(okvir.dataset.plocice, { referrerPolicy: UPUCIVAC_PLOCICA,
        maxZoom: parseInt(okvir.dataset.najviseZ, 10) || 17, attribution: zaslugeKarte(okvir) });
      var promasaja = 0;
      osnovni.on('tileerror', function () { if (++promasaja >= 3) okvir.querySelector('.karta-bez-mreze').hidden = false; });
      podloga = cipoviPodloga(karta, okvir, osnovni, stanje.podloga, function (p) { podloga = p; zapisi(); });
    }

    var slojevi = {};
    var poruka = okvir.querySelector('.karta-ucitava');
    var podaci = fetch(okvir.dataset.podaci, { credentials: 'same-origin' }).then(function (r) {
      if (!r.ok) throw new Error(r.status);
      return r.json();
    }).then(function (d) {
      var objekti = slojeviObjekata(d.objekti || {});
      Object.keys(objekti).forEach(function (k) { slojevi[k] = objekti[k]; });
      slojevi.vodotoci = slojVodotoka(d.vodotoci || {});
      slojevi.letve = slojLetvi(d.letve || []);
    });
    var zupanije = okvir.dataset.zupanije ? fetch(okvir.dataset.zupanije, { credentials: 'same-origin' }).then(function (r) {
      return r.ok ? r.json() : null;
    }).then(function (z) { if (z) slojevi.zupanije = slojZupanija(z); }).catch(function () {}) : Promise.resolve();

    Promise.all([podaci, zupanije]).then(function () {
      poruka.hidden = true;
      primijeniSlojeve(karta, okvir, slojevi);
      pocetniPogled(karta, stanje, slojevi);
      smanjiIkone(karta, okvir, slojevi);
      karta.on('zoomend', function () { smanjiIkone(karta, okvir, slojevi); });
      karta.on('moveend', zapisi);
    }).catch(function () {
      poruka.textContent = 'Ucrtani slojevi se nisu učitali. Osvježite stranicu.';
    });

    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) {
      s.addEventListener('change', function () { primijeniSlojeve(karta, okvir, slojevi); zapisi(); });
    });
    okvir.querySelectorAll('[data-skupina-prekidac]').forEach(function (sve) {
      sve.addEventListener('change', function () {
        sve.closest('.karta-skupina').querySelectorAll('input[data-sloj]').forEach(function (s) { s.checked = sve.checked; });
        primijeniSlojeve(karta, okvir, slojevi);
        zapisi();
      });
    });
    okvir.querySelector('.karta-vrati').addEventListener('click', function () {
      okvir.querySelectorAll('input[data-sloj]').forEach(function (s) { s.checked = true; });
      primijeniSlojeve(karta, okvir, slojevi);
      zapisi();
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    if (typeof L === 'undefined') return;
    document.querySelectorAll('.karta-stranica').forEach(pokreni);
  });
})();
