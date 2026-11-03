// Karta: sve ucrtano na jednoj karti, sa slojevima koji se pale i gase
// (karta.html). Crte i točke dohvaća s data-podaci, granice županija s
// data-zupanije; boje i ikone vrsta su iz Administracije › Karte
// (postavkeKarte u app.js). Adresa pamti položaj, podlogu, isključene
// slojeve i otvoreni popis: #poz=45.6,18.7,10&podloga=Satelit&bez=letve&nedostaje=nasipi
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

  function bojaPribliznog() {
    return (getComputedStyle(document.documentElement).getPropertyValue('--karta-priblizno') || '').trim() || '#e8590c';
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
      bez: (p.get('bez') || '').split(',').filter(Boolean),
      nedostaje: p.get('nedostaje') || ''
    };
  }

  function zapisiAdresu(karta, okvir, podloga) {
    var c = karta.getCenter(), p = new URLSearchParams();
    p.set('poz', c.lat.toFixed(5) + ',' + c.lng.toFixed(5) + ',' + karta.getZoom());
    if (podloga) p.set('podloga', podloga);
    var bez = [];
    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) { if (!s.checked) bez.push(s.dataset.sloj); });
    if (bez.length) p.set('bez', bez.join(','));
    if (okvir.dataset.nedostaje) p.set('nedostaje', okvir.dataset.nedostaje);
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
    okvir.querySelectorAll('.karta-slojevi-zatvori').forEach(function (z) { z.addEventListener('click', function () { otvori(false); }); });
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

  // Što nedostaje: popis onoga što nema crtu ili položaj, po području, s
  // iscrtkanim približnim mjestom po stacionaži (karta_nedostaje.go). Crta
  // ide nizvodno, pa se nasip lijeve obale pomakne ulijevo od toka, a desne
  // udesno; pomak je u točkama zaslona i računa se iznova pri zumiranju. Na
  // pregledu sektora pomak bi bio nekoliko kilometara, pa crta ostaje na toku.
  var POMAK_OBALE = 7, POMAK_OD_ZUMA = 12;

  function pomaknutaCrta(karta, tocke, px) {
    var t = tocke.map(function (ll) { return karta.latLngToLayerPoint(ll); });
    return t.map(function (p, i) {
      var a = t[Math.max(0, i - 1)], b = t[Math.min(t.length - 1, i + 1)];
      var dx = b.x - a.x, dy = b.y - a.y, d = Math.sqrt(dx * dx + dy * dy) || 1;
      // lijevo od smjera toka; na zaslonu y raste prema dolje
      return karta.layerPointToLatLng(L.point(p.x + dy / d * px, p.y - dx / d * px));
    });
  }

  function poljeCsv(v) {
    v = String(v == null ? '' : v);
    if (/^[=+\-@]/.test(v)) v = "'" + v;
    return /[";\r\n]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v;
  }

  function pogledNedostaje(okvir, karta, podaci, promjena) {
    var pogled = okvir.querySelector('.karta-pogled-nedostaje');
    var slojeviPogled = okvir.querySelector('.karta-pogled-slojevi');
    var cipovi = pogled.querySelector('.karta-nedostaje-cipovi');
    var popis = pogled.querySelector('.karta-nedostaje-popis');
    var napomena = pogled.querySelector('.karta-nedostaje-napomena');
    var grupa = L.featureGroup(), crte = [], mjesta = {};
    var sloj = null, odabrano = '';
    var sve = podaci.nedostaje || [];

    function nacrtaj(kljuc) {
      var b = bojaPribliznog();
      grupa.clearLayers();
      crte = [];
      mjesta = {};
      ((podaci.priblizno || {}).features || []).forEach(function (f) {
        var p = f.properties, c = f.geometry.coordinates, l, granice;
        if (p.sloj !== kljuc) return;
        if (f.geometry.type === 'LineString') {
          var tocke = c.map(function (x) { return L.latLng(x[1], x[0]); });
          l = L.polyline(tocke, { color: b, weight: 3, dashArray: '6 5', opacity: 0.95 });
          granice = L.latLngBounds(tocke);
          crte.push({ l: l, tocke: tocke, pomak: p.obala === 'L' ? POMAK_OBALE : p.obala === 'D' ? -POMAK_OBALE : 0 });
        } else {
          l = L.circleMarker([c[1], c[0]], { radius: 9, color: b, weight: 2, dashArray: '3 3', fillColor: b, fillOpacity: 0.15 });
          granice = L.latLngBounds([[c[1], c[0]], [c[1], c[0]]]);
        }
        l.granice = granice;
        l.bindTooltip(escapeHtml(p.naziv) + ' · približno', { sticky: true }).bindPopup(prozor(p)).addTo(grupa);
        l.on('mouseover', function () { istakni(p.adresa, true); });
        l.on('mouseout', function () { istakni(p.adresa, false); });
        (mjesta[p.adresa] = mjesta[p.adresa] || []).push(l);
      });
      pomakni();
    }

    function pomakni() {
      var uz = karta.getZoom() >= POMAK_OD_ZUMA;
      crte.forEach(function (c) { c.l.setLatLngs(uz && c.pomak ? pomaknutaCrta(karta, c.tocke, c.pomak) : c.tocke); });
    }

    function istakni(adresa, da) {
      (mjesta[adresa] || []).forEach(function (l) {
        l.setStyle({ weight: da ? 6 : (l.setRadius ? 2 : 3) });
        if (da) l.bringToFront();
      });
      popis.querySelectorAll('.karta-nedostaje-stavka').forEach(function (s) {
        if (s.dataset.adresa === adresa) s.classList.toggle('istaknuto', da);
      });
    }

    function pokazi(adresa) {
      var granice = L.latLngBounds([]);
      (mjesta[adresa] || []).forEach(function (l) { granice.extend(l.granice); });
      if (!granice.isValid()) return;
      // na tabletu i mobitelu ploča pokriva kartu, pa se zatvori
      var uski = window.matchMedia(USKI).matches;
      if (uski) pogled.querySelector('.karta-slojevi-zatvori').click();
      karta.fitBounds(granice, { paddingTopLeft: [24, 24], maxZoom: 15,
        paddingBottomRight: [uski ? 24 : 384 + 24, uski ? 96 : 24] });
    }

    function stavke() {
      return sve.filter(function (r) { return r.sloj === sloj.kljuc && (!odabrano || r.skupina === odabrano); });
    }

    function popuni() {
      var brojevi = {}, redom = [];
      sve.forEach(function (r) {
        if (r.sloj !== sloj.kljuc) return;
        if (!brojevi[r.skupina]) { brojevi[r.skupina] = 0; redom.push(r.skupina); }
        brojevi[r.skupina]++;
      });
      cipovi.innerHTML = '';
      if (redom.length > 1) {
        redom.forEach(function (s) {
          var c = document.createElement('button');
          c.type = 'button';
          c.className = 'karta-cip';
          c.title = s;
          c.textContent = s.split(' · ')[0] + ' · ' + brojevi[s];
          c.setAttribute('aria-pressed', s === odabrano ? 'true' : 'false');
          c.addEventListener('click', function () { odabrano = odabrano === s ? '' : s; popuni(); });
          cipovi.appendChild(c);
        });
      }
      popis.innerHTML = '';
      var skupina = null;
      stavke().forEach(function (r) {
        if (r.skupina !== skupina) {
          skupina = r.skupina;
          var h = document.createElement('h3');
          h.className = 'karta-nedostaje-skupina';
          h.textContent = skupina;
          popis.appendChild(h);
        }
        var s = document.createElement('div');
        s.className = 'karta-nedostaje-stavka';
        s.dataset.adresa = r.adresa;
        var naziv = document.createElement(r.mjesto ? 'button' : 'span');
        naziv.className = 'karta-nedostaje-naziv';
        naziv.textContent = r.naziv;
        if (r.mjesto) {
          naziv.type = 'button';
          naziv.title = 'Pokaži približno mjesto';
          naziv.addEventListener('click', function () { pokazi(r.adresa); });
        }
        var gdje = document.createElement('span');
        gdje.className = 'karta-nedostaje-gdje';
        gdje.textContent = r.gdje;
        var tekst = document.createElement('div');
        tekst.className = 'karta-nedostaje-tekst';
        tekst.appendChild(naziv);
        tekst.appendChild(gdje);
        var ucrtaj = document.createElement('a');
        ucrtaj.className = 'karta-nedostaje-ucrtaj';
        ucrtaj.href = r.adresa;
        ucrtaj.textContent = 'Ucrtaj';
        ucrtaj.setAttribute('aria-label', 'Ucrtaj: ' + r.naziv);
        s.appendChild(tekst);
        s.appendChild(ucrtaj);
        s.addEventListener('mouseenter', function () { istakni(r.adresa, true); });
        s.addEventListener('mouseleave', function () { istakni(r.adresa, false); });
        popis.appendChild(s);
      });
    }

    function otvori(gumb) {
      var crta = gumb.dataset.crta === 'true';
      sloj = { kljuc: gumb.dataset.nedostaje, naziv: gumb.dataset.naziv };
      odabrano = '';
      var broj = sve.filter(function (r) { return r.sloj === sloj.kljuc; });
      var bez = sloj.kljuc === 'vodotoci' ? 'bez toka' : crta ? 'bez crte' : 'bez položaja';
      pogled.querySelector('.karta-nedostaje-sazetak').textContent =
        sloj.naziv + ' ' + bez + ': ' + broj.length + ' od ' + gumb.dataset.ukupno;
      var ima = broj.some(function (r) { return r.mjesto; });
      napomena.hidden = !ima;
      napomena.classList.toggle('karta-nedostaje-tocka', !crta);
      napomena.querySelector('.karta-nedostaje-napomena-tekst').textContent = crta
        ? 'Iscrtkano je približno mjesto po stacionaži iz dionice. To nije crta nasipa.'
        : 'Iscrtkani krug je približno mjesto po stacionaži. To nije ucrtani položaj.';
      slojeviPogled.hidden = true;
      pogled.hidden = false;
      okvir.dataset.nedostaje = sloj.kljuc;
      nacrtaj(sloj.kljuc);
      grupa.addTo(karta);
      popuni();
      promjena();
    }

    function zatvori() {
      sloj = null;
      pogled.hidden = true;
      slojeviPogled.hidden = false;
      delete okvir.dataset.nedostaje;
      karta.removeLayer(grupa);
      promjena();
    }

    function preuzmi() {
      var redci = [['Područje', 'Naziv', 'Gdje', 'Stranica']];
      stavke().forEach(function (r) { redci.push([r.skupina, r.naziv, r.gdje, location.origin + r.adresa]); });
      var csv = '\ufeff' + redci.map(function (r) { return r.map(poljeCsv).join(';'); }).join('\r\n');
      var a = document.createElement('a');
      a.href = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
      a.download = 'nedostaje-' + sloj.kljuc + '.csv';
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(function () { URL.revokeObjectURL(a.href); }, 0);
    }

    karta.on('zoomend viewreset', pomakni);
    okvir.querySelectorAll('[data-nedostaje]').forEach(function (g) {
      g.addEventListener('click', function () { otvori(g); pogled.querySelector('.karta-nedostaje-natrag').focus(); });
    });
    pogled.querySelector('.karta-nedostaje-natrag').addEventListener('click', function () {
      var kljuc = sloj && sloj.kljuc;
      zatvori();
      var g = kljuc && okvir.querySelector('[data-nedostaje="' + kljuc + '"]');
      if (g) g.focus();
    });
    pogled.querySelector('.karta-nedostaje-preuzmi').addEventListener('click', preuzmi);
    return {
      otvori: function (kljuc) {
        var g = okvir.querySelector('[data-nedostaje="' + kljuc + '"]');
        if (g) otvori(g);
      }
    };
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

    var slojevi = {}, podaciKarte = {};
    var poruka = okvir.querySelector('.karta-ucitava');
    var podaci = fetch(okvir.dataset.podaci, { credentials: 'same-origin' }).then(function (r) {
      if (!r.ok) throw new Error(r.status);
      return r.json();
    }).then(function (d) {
      podaciKarte = d;
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
      var nedostaje = pogledNedostaje(okvir, karta, podaciKarte, zapisi);
      if (stanje.nedostaje) nedostaje.otvori(stanje.nedostaje);
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
