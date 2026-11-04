// Karta: sve ucrtano na jednoj karti, sa slojevima koji se pale i gase
// (karta.html). Crte i točke dohvaća s data-podaci, granice područja s
// data-podrucja; boje i ikone vrsta su iz Administracije › Karte
// (postavkeKarte u app.js). Adresa pamti položaj, podlogu, isključene
// slojeve i otvoreni popis: #poz=45.6,18.7,10&podloga=Satelit&bez=letve&nedostaje=nasipi
// Crta se i ispravlja samo ovdje (karta-uredjivanje.js); stranice objekta i
// vode otvaraju Kartu s #uredi=objekt:<id>, #uredi=vodotok:<šifra> ili
// #uredi=letva:<id>.
(function () {
  var ZADANO = { oko: [45.55, 18.7], zum: 9 };
  var USKI = '(max-width: 839px)';
  var urediNaKarti = null; // uređivač na Karti, kad ga prijavljeni smije koristiti

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

  // prozorčić na karti: naziv, stranica i, kad prijavljeni smije, uređivanje
  function prozor(p, uredi) {
    return function () {
      var d = document.createElement('div');
      d.className = 'karta-prozor';
      d.innerHTML = '<strong>' + escapeHtml(p.naziv || '') + '</strong>' +
        (p.adresa ? '<br><a href="' + escapeHtml(p.adresa) + '">Otvori stranicu</a>' : '');
      // dok se nešto crta, drugo se ne otvara za uređivanje
      if (p.uredjuje && urediNaKarti && !urediNaKarti.radi()) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'btn btn-sm btn-secondary karta-prozor-uredi';
        b.textContent = 'Uredi na karti';
        b.addEventListener('click', function () { urediNaKarti.uredi(uredi ? uredi() : p); });
        d.appendChild(b);
      }
      return d;
    };
  }

  // stanje iz adrese: položaj, podloga i isključeni slojevi
  function procitajAdresu() {
    var p = new URLSearchParams(location.hash.replace(/^#/, ''));
    var poz = (p.get('poz') || '').split(',').map(parseFloat);
    return {
      poz: poz.length === 3 && poz.every(isFinite) ? poz : null,
      podloga: p.get('podloga') || '',
      bez: (p.get('bez') || '').split(',').filter(Boolean),
      uz: (p.get('uz') || '').split(',').filter(Boolean), // slojevi koji su zadano isključeni, a uključeni su
      nedostaje: p.get('nedostaje') || '',
      popis: p.get('popis') || '', // ucrtano: drugi popis u pogledu opširnije
      uredi: p.get('uredi') || '' // objekt:<id>, vodotok:<šifra>, letva:<id>
    };
  }

  function zapisiAdresu(karta, okvir, podloga) {
    var c = karta.getCenter(), p = new URLSearchParams();
    p.set('poz', c.lat.toFixed(5) + ',' + c.lng.toFixed(5) + ',' + karta.getZoom());
    if (podloga) p.set('podloga', podloga);
    var bez = [], uz = [];
    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) {
      var zadano = s.dataset.zadano !== 'false';
      if (!s.checked && zadano) bez.push(s.dataset.sloj);
      if (s.checked && !zadano) uz.push(s.dataset.sloj);
    });
    if (bez.length) p.set('bez', bez.join(','));
    if (uz.length) p.set('uz', uz.join(','));
    if (okvir.dataset.nedostaje) p.set('nedostaje', okvir.dataset.nedostaje);
    if (okvir.dataset.nedostaje && okvir.dataset.popis === 'ucrtano') p.set('popis', 'ucrtano');
    history.replaceState(null, '', '#' + p.toString().replace(/%2C/g, ','));
  }

  // Prianjanje pri crtanju (Geoman u načinu opt-in): crte i točke prikaza
  // ne uređuju se, ali se nova crta i pomaknuti vrh lijepe na njih
  // (snapIgnore: false), pa ušće kanala sjedne na tok, a kraj nasipa na
  // crpnu stanicu. Približna mjesta i granice županija nisu stvarne crte,
  // pa se na njih ne lijepi.
  function zaPrianjanje(l) {
    l.options.snapIgnore = false;
    return l;
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
        snapIgnore: false,
        style: function (f) {
          var stil = (postavkeKarte().objekti || {})[f.properties.vrsta] || {};
          return { color: boja(f.properties.vrsta), weight: stil.debljina || 4, opacity: 0.9 };
        },
        pointToLayer: function (f, ll) {
          return zaPrianjanje(tockaObjekta(ll, (postavkeKarte().objekti || {})[f.properties.vrsta] || {}, boja(f.properties.vrsta)));
        },
        onEachFeature: function (f, l) { l.bindPopup(prozor(f.properties)); }
      });
    });
    return out;
  }

  // letve imaju svoju ikonu (stupić letve ili ikonu iz Administracije ›
  // Karte); na pregledu sektora ikona se smanji (smanjiIkone)
  function slojLetvi(letve) {
    var grupa = L.featureGroup();
    letve.forEach(function (l) {
      var m = L.marker([l.lat, l.lon], { icon: stvoriLetvaIkonu(), riseOnHover: true, snapIgnore: false });
      m.podaci = l;
      m.bindTooltip(escapeHtml(l.naziv), { direction: 'top', offset: [0, -36] })
        .bindPopup(prozor(l, function () {
          return { vrsta: 'letva', id: l.id, naziv: l.naziv, adresa: l.adresa, ll: m.getLatLng() };
        })).addTo(grupa);
    });
    return grupa;
  }

  // vodotoci po redu vode (handlers_karta.go): veći red, deblja crta
  var DEBLJINA_VODE = { 'vodotoci-medudrzavni': 4.5, 'vodotoci-i': 3.5, 'vodotoci-ii': 2.5, 'vodotoci-iii-iv': 2, 'vodotoci-bez-reda': 2 };

  function jeSlojVode(k) { return k.indexOf('vodotoci') === 0; }

  function slojeviVodotoka(zbirka) {
    var poSlojevima = {}, out = {};
    (zbirka.features || []).forEach(function (f) {
      (poSlojevima[f.properties.sloj] = poSlojevima[f.properties.sloj] || []).push(f);
    });
    Object.keys(poSlojevima).forEach(function (k) {
      out[k] = L.geoJSON({ type: 'FeatureCollection', features: poSlojevima[k] }, {
        snapIgnore: false,
        style: function () { return { color: bojaRijeke(), weight: DEBLJINA_VODE[k] || 2.5, opacity: 0.85, className: 'rijeka-linija' }; },
        onEachFeature: function (f, l) { l.bindPopup(prozor(f.properties)); }
      });
    });
    return out;
  }

  // Područja: granica i ime, a ime se vidi samo na povećanju na kojem ima
  // smisla (županije na pregledu, općine izbliza). Granice dolaze s
  // /karta/podrucja/ pojednostavljene (karta_podrucja.go).
  var PODRUCJA = {
    zupanije: { stil: { color: '#8e909a', weight: 1.5, dashArray: '6 4' }, natpisi: [7, 10] },
    opcine: { stil: { color: '#9ca3af', weight: 1, dashArray: '2 3' }, natpisi: [11, 19] },
    'branjena-podrucja': { stil: { color: '#7c3aed', weight: 2, dashArray: '8 4 2 4', opacity: 0.8 }, natpisi: [8, 12] }
  };

  var SlojPodrucja = L.FeatureGroup.extend({
    initialize: function (granice, natpisi, raspon) {
      L.FeatureGroup.prototype.initialize.call(this, [granice]);
      this._natpisi = natpisi;
      this._raspon = raspon;
    },
    onAdd: function (map) {
      L.FeatureGroup.prototype.onAdd.call(this, map);
      map.on('zoomend', this._zum, this);
      this._zum();
    },
    onRemove: function (map) {
      map.off('zoomend', this._zum, this);
      map.removeLayer(this._natpisi);
      L.FeatureGroup.prototype.onRemove.call(this, map);
    },
    _zum: function () {
      var z = this._map.getZoom();
      if (z >= this._raspon[0] && z <= this._raspon[1]) this._natpisi.addTo(this._map);
      else this._map.removeLayer(this._natpisi);
    }
  });

  function slojPodrucja(kljuc, zbirka) {
    var o = PODRUCJA[kljuc];
    var granice = L.geoJSON(zbirka, {
      snapIgnore: true,
      style: function () { return L.extend({ fill: false, interactive: false }, o.stil); }
    });
    var natpisi = L.layerGroup();
    (zbirka.features || []).forEach(function (f) {
      var t = f.properties.tocka;
      if (!t) return;
      L.marker([t[1], t[0]], { interactive: false, keyboard: false, snapIgnore: true,
        icon: L.divIcon({ className: 'karta-natpis-podrucja karta-natpis-' + kljuc, iconSize: null,
          html: '<span title="' + escapeHtml(f.properties.naziv) + '">' + escapeHtml(f.properties.kratko || f.properties.naziv) + '</span>' }) }).addTo(natpisi);
    });
    return new SlojPodrucja(granice, natpisi, o.natpisi);
  }

  // granice područja učitavaju se tek kad se sloj uključi (općine su velike)
  var ucitavaPodrucje = {};
  function ucitajPodrucje(karta, okvir, slojevi, kljuc) {
    if (ucitavaPodrucje[kljuc] || !okvir.dataset.podrucja) return;
    ucitavaPodrucje[kljuc] = true;
    fetch(okvir.dataset.podrucja + kljuc + '.geojson', { credentials: 'same-origin' }).then(function (r) {
      return r.ok ? r.json() : null;
    }).then(function (z) {
      if (!z) return;
      slojevi[kljuc] = slojPodrucja(kljuc, z);
      primijeniSlojeve(karta, okvir, slojevi);
    }).catch(function () {}).then(function () { ucitavaPodrucje[kljuc] = false; });
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
      if (!sloj && PODRUCJA[s.dataset.sloj] && s.checked) ucitajPodrucje(karta, okvir, slojevi, s.dataset.sloj);
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

  function plocaSlojeva(okvir) {
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
      if (DEBLJINA_VODE[z.dataset.slojZnak]) z.style.setProperty('--karta-debljina', Math.round(DEBLJINA_VODE[z.dataset.slojZnak]) + 'px');
    });
    return { otvori: function () { otvori(true); } };
  }

  // ikone se smanjuju kad se karta odzumira, da se na pregledu sektora ne
  // preklapaju; od 13. zuma su pune veličine
  function mjeriloIkona(zum) {
    return zum >= 13 ? 1 : zum >= 11 ? 0.8 : zum >= 9 ? 0.6 : 0.45;
  }

  function smanjiIkone(karta, okvir) {
    okvir.style.setProperty('--karta-ikona', mjeriloIkona(karta.getZoom()));
  }

  // broj uz sloj i gumb „što nedostaje” nakon spremanja na Karti
  function osvjeziBrojeve(okvir, brojevi) {
    okvir.querySelectorAll('.karta-sloj-broj[data-opis]').forEach(function (b) {
      var n = (brojevi || {})[b.dataset.brojSloja];
      if (!n || !n[1]) return;
      b.textContent = n[0].toLocaleString('hr-HR') + ' od ' + n[1].toLocaleString('hr-HR') + ' ' + b.dataset.opis;
      var g = okvir.querySelector('[data-nedostaje="' + b.dataset.brojSloja + '"]');
      if (g) g.dataset.ukupno = n[1];
    });
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

  // Opširnije o sloju: dva popisa, „Nedostaje” i „Ucrtano” (Odluke
  // (4. 11. 2026.)). Ono što nedostaje ima iscrtkano približno mjesto i gumb
  // Ucrtaj, a ucrtano Pokaži i Uredi.
  function pogledNedostaje(okvir, karta, podaci, promjena, slojevi) {
    var pogled = okvir.querySelector('.karta-pogled-nedostaje');
    var slojeviPogled = okvir.querySelector('.karta-pogled-slojevi');
    var cipovi = pogled.querySelector('.karta-nedostaje-cipovi');
    var popis = pogled.querySelector('.karta-nedostaje-popis');
    var napomena = pogled.querySelector('.karta-nedostaje-napomena');
    var grupa = L.featureGroup(), crte = [], mjesta = {};
    var sloj = null, odabrano = '', vrsta = 'nedostaje';
    function sve() { return podaci[vrsta] || []; }

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
          l = L.polyline(tocke, { color: b, weight: 3, dashArray: '6 5', opacity: 0.95, snapIgnore: true });
          granice = L.latLngBounds(tocke);
          crte.push({ l: l, tocke: tocke, pomak: p.obala === 'L' ? POMAK_OBALE : p.obala === 'D' ? -POMAK_OBALE : 0 });
        } else {
          l = L.circleMarker([c[1], c[0]], { radius: 9, color: b, weight: 2, dashArray: '3 3', fillColor: b, fillOpacity: 0.15, snapIgnore: true });
          granice = L.latLngBounds([[c[1], c[0]], [c[1], c[0]]]);
        }
        l.granice = granice;
        l.bindTooltip(escapeHtml(p.naziv) + ' · približno', { sticky: true }).bindPopup(prozor({ naziv: p.naziv, adresa: p.adresa })).addTo(grupa);
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

    function graniceMjesta(adresa) {
      var granice = L.latLngBounds([]);
      (mjesta[adresa] || []).forEach(function (l) { granice.extend(l.granice); });
      return granice;
    }

    // granice ucrtanog zapisa iz sloja prikaza
    function graniceUcrtanog(adresa) {
      var granice = L.latLngBounds([]), s = slojevi[sloj.kljuc];
      if (s && s.eachLayer) s.eachLayer(function (l) {
        var p = (l.feature && l.feature.properties) || l.podaci || {};
        if (p.adresa !== adresa) return;
        if (l.getBounds) granice.extend(l.getBounds());
        else if (l.getLatLng) granice.extend(l.getLatLng());
      });
      return granice;
    }

    function pokazi(adresa) {
      var granice = vrsta === 'ucrtano' ? graniceUcrtanog(adresa) : graniceMjesta(adresa);
      if (!granice.isValid()) return;
      // na tabletu i mobitelu ploča pokriva kartu, pa se zatvori
      var uski = window.matchMedia(USKI).matches;
      if (uski) pogled.querySelector('.karta-slojevi-zatvori').click();
      karta.fitBounds(granice, { paddingTopLeft: [24, 24], maxZoom: 15,
        paddingBottomRight: [uski ? 24 : 384 + 24, uski ? 96 : 24] });
    }

    function stavke() {
      return sve().filter(function (r) { return r.sloj === sloj.kljuc && (!odabrano || r.skupina === odabrano); });
    }

    // gumb uz redak: Ucrtaj ili Uredi na Karti, a bez prava Otvori stranicu
    function gumbStavke(r) {
      var smije = r.uredjuje && urediNaKarti;
      var g = document.createElement(smije ? 'button' : 'a');
      g.className = 'karta-nedostaje-ucrtaj';
      g.textContent = !smije ? 'Otvori' : vrsta === 'ucrtano' ? 'Uredi' : 'Ucrtaj';
      g.setAttribute('aria-label', g.textContent + ': ' + r.naziv);
      if (!smije) { g.href = r.adresa; return g; }
      g.type = 'button';
      g.addEventListener('click', function () {
        var p = { id: r.id, kod: r.kod, vrsta: r.vrsta === 'LETVA' ? 'letva' : r.vrsta, naziv: r.naziv, adresa: r.adresa,
          granice: vrsta === 'ucrtano' ? graniceUcrtanog(r.adresa) : graniceMjesta(r.adresa) };
        if (p.vrsta === 'letva' && p.granice.isValid()) p.ll = p.granice.getCenter();
        urediNaKarti.uredi(p);
      });
      return g;
    }

    function popuni() {
      var brojevi = {}, redom = [];
      pogled.querySelectorAll('.karta-popis-vrsta').forEach(function (k) {
        var n = (podaci[k.dataset.popis] || []).filter(function (r) { return r.sloj === sloj.kljuc; }).length;
        k.querySelector('.karta-popis-broj').textContent = n.toLocaleString('hr-HR');
        k.setAttribute('aria-selected', k.dataset.popis === vrsta ? 'true' : 'false');
      });
      sve().forEach(function (r) {
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
      if (!stavke().length) {
        var prazno = document.createElement('p');
        prazno.className = 'karta-nedostaje-prazno';
        prazno.textContent = vrsta === 'ucrtano' ? 'Još ništa nije ucrtano.' : 'Sve je ucrtano.';
        popis.appendChild(prazno);
      }
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
        var pokaziMjesto = vrsta === 'ucrtano' || r.mjesto;
        var naziv = document.createElement(pokaziMjesto ? 'button' : 'span');
        naziv.className = 'karta-nedostaje-naziv';
        naziv.textContent = r.naziv;
        if (pokaziMjesto) {
          naziv.type = 'button';
          naziv.title = vrsta === 'ucrtano' ? 'Pokaži na karti' : 'Pokaži približno mjesto';
          naziv.addEventListener('click', function () { pokazi(r.adresa); });
        }
        var gdje = document.createElement('span');
        gdje.className = 'karta-nedostaje-gdje';
        gdje.textContent = r.gdje;
        var tekst = document.createElement('div');
        tekst.className = 'karta-nedostaje-tekst';
        tekst.appendChild(naziv);
        tekst.appendChild(gdje);
        s.appendChild(tekst);
        s.appendChild(gumbStavke(r));
        s.addEventListener('mouseenter', function () { istakni(r.adresa, true); });
        s.addEventListener('mouseleave', function () { istakni(r.adresa, false); });
        popis.appendChild(s);
      });
    }

    // popis koji se pokazuje: zadano ono što nedostaje, a kad nema ništa,
    // ono što je ucrtano
    function odaberiVrstu(v) {
      vrsta = v;
      odabrano = '';
      okvir.dataset.popis = vrsta;
      var crta = sloj.crta;
      var ima = vrsta === 'nedostaje' && sve().some(function (r) { return r.sloj === sloj.kljuc && r.mjesto; });
      napomena.hidden = !ima;
      if (vrsta === 'nedostaje') grupa.addTo(karta); else karta.removeLayer(grupa);
      napomena.classList.toggle('karta-nedostaje-tocka', !crta);
      napomena.querySelector('.karta-nedostaje-napomena-tekst').textContent = crta
        ? 'Iscrtkano je približno mjesto po stacionaži iz dionice. To nije crta nasipa.'
        : 'Iscrtkani krug je približno mjesto po stacionaži. To nije ucrtani položaj.';
      popuni();
      promjena();
    }

    function otvori(gumb, zeljena) {
      sloj = { kljuc: gumb.dataset.nedostaje, naziv: gumb.dataset.naziv, crta: gumb.dataset.crta === 'true' };
      var nedostaje = (podaci.nedostaje || []).filter(function (r) { return r.sloj === sloj.kljuc; }).length;
      var ucrtano = (podaci.ucrtano || []).filter(function (r) { return r.sloj === sloj.kljuc; }).length;
      pogled.querySelector('.karta-nedostaje-naslov').textContent = sloj.naziv;
      pogled.querySelector('.karta-nedostaje-sazetak').textContent =
        (sloj.kljuc === 'letve' ? 'S položajem ' : 'Ucrtano ') + ucrtano.toLocaleString('hr-HR') + ' od ' + (ucrtano + nedostaje).toLocaleString('hr-HR') + '.';
      slojeviPogled.hidden = true;
      pogled.hidden = false;
      okvir.dataset.nedostaje = sloj.kljuc;
      nacrtaj(sloj.kljuc);
      odaberiVrstu(zeljena || (nedostaje ? 'nedostaje' : 'ucrtano'));
    }

    function zatvori() {
      sloj = null;
      pogled.hidden = true;
      slojeviPogled.hidden = false;
      delete okvir.dataset.nedostaje;
      delete okvir.dataset.popis;
      karta.removeLayer(grupa);
      promjena();
    }

    function preuzmi() {
      var redci = [['Područje', 'Naziv', 'Gdje', 'Stranica']];
      stavke().forEach(function (r) { redci.push([r.skupina, r.naziv, r.gdje, location.origin + r.adresa]); });
      var csv = '\ufeff' + redci.map(function (r) { return r.map(poljeCsv).join(';'); }).join('\r\n');
      var a = document.createElement('a');
      a.href = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
      a.download = vrsta + '-' + sloj.kljuc + '.csv';
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
    pogled.querySelectorAll('.karta-popis-vrsta').forEach(function (k) {
      k.addEventListener('click', function () { if (sloj) odaberiVrstu(k.dataset.popis); });
    });
    return {
      otvori: function (kljuc, zeljena) {
        var g = okvir.querySelector('[data-nedostaje="' + kljuc + '"]');
        if (g) otvori(g, zeljena === 'ucrtano' ? 'ucrtano' : '');
      },
      // nakon spremanja: novi popisi i približna mjesta, isti otvoreni sloj i popis
      osvjezi: function (novi) {
        podaci = novi;
        if (!sloj) return;
        var g = okvir.querySelector('[data-nedostaje="' + sloj.kljuc + '"]'), bio = odabrano;
        otvori(g, vrsta);
        odabrano = bio;
        popuni();
      },
      nadji: function (id, kod) {
        return (podaci.nedostaje || []).concat(podaci.ucrtano || []).filter(function (r) { return (id && r.id === id) || (kod && r.kod === kod); })[0];
      },
      granice: graniceMjesta
    };
  }

  function pocetniPogled(karta, stanje, slojevi) {
    if (stanje.poz) { karta.setView([stanje.poz[0], stanje.poz[1]], stanje.poz[2]); return; }
    var granice = L.latLngBounds([]);
    Object.keys(slojevi).forEach(function (k) {
      if (!jeSlojVode(k) && k !== 'letve' && !PODRUCJA[k] && slojevi[k].getBounds) granice.extend(slojevi[k].getBounds());
    });
    if (!granice.isValid() && slojevi.letve) granice = slojevi.letve.getBounds();
    if (!granice.isValid()) { karta.setView(ZADANO.oko, ZADANO.zum); return; }
    var desno = window.matchMedia(USKI).matches ? 0 : 384;
    karta.fitBounds(granice, { paddingTopLeft: [24, 24], paddingBottomRight: [desno + 24, 24], maxZoom: 13 });
  }

  // uređivanje s adrese: stranica objekta ili vode šalje na Kartu
  function urediSAdrese(uredi, podaci, nedostaje) {
    var i = uredi.indexOf(':'), vrsta = uredi.slice(0, i), kljuc = uredi.slice(i + 1);
    if (!urediNaKarti || i < 0 || !kljuc) return;
    if (vrsta === 'vodotok') { urediNaKarti.uredi({ kod: kljuc }); return; }
    if (vrsta === 'letva') {
      var l = (podaci.letve || []).filter(function (x) { return x.id === kljuc; })[0];
      var r = nedostaje.nadji(kljuc);
      if (l) urediNaKarti.uredi({ vrsta: 'letva', id: l.id, naziv: l.naziv, adresa: l.adresa, ll: L.latLng(l.lat, l.lon) });
      else if (r) urediNaKarti.uredi({ vrsta: 'letva', id: r.id, naziv: r.naziv, adresa: r.adresa });
      return;
    }
    if (vrsta === 'objekt') urediNaKarti.uredi({ id: kljuc });
  }

  function pokreni(okvir) {
    var platno = okvir.querySelector('.karta-platno');
    var stanje = procitajAdresu();
    // Geoman uređuje samo ono što uređivač učita, nikad slojeve prikaza
    if (L.PM) L.PM.setOptIn(true);
    okvir.querySelectorAll('input[data-sloj]').forEach(function (s) {
      if (stanje.bez.indexOf(s.dataset.sloj) >= 0) s.checked = false;
      if (stanje.uz.indexOf(s.dataset.sloj) >= 0) s.checked = true;
    });
    // u načinu opt-in i karta mora reći da Geoman smije na nju
    var karta = L.map(platno, { scrollWheelZoom: true, zoomControl: true, pmIgnore: false });
    karta.setView(ZADANO.oko, ZADANO.zum);
    dodajKontroleKarte(karta, platno, { fullscreenTarget: okvir });
    var ploca = plocaSlojeva(okvir);

    var podloga = '';
    function zapisi() { zapisiAdresu(karta, okvir, podloga); }
    if (okvir.dataset.plocice) {
      var osnovni = L.tileLayer(okvir.dataset.plocice, { referrerPolicy: UPUCIVAC_PLOCICA,
        maxZoom: parseInt(okvir.dataset.najviseZ, 10) || 17, attribution: zaslugeKarte(okvir) });
      var promasaja = 0;
      osnovni.on('tileerror', function () { if (++promasaja >= 3) okvir.querySelector('.karta-bez-mreze').hidden = false; });
      podloga = cipoviPodloga(karta, okvir, osnovni, stanje.podloga, function (p) { podloga = p; zapisi(); });
    }

    var slojevi = {}, podaciKarte = {}, nedostaje = null;
    var poruka = okvir.querySelector('.karta-ucitava');
    function dohvati() {
      return fetch(okvir.dataset.podaci, { credentials: 'same-origin' }).then(function (r) {
        if (!r.ok) throw new Error(r.status);
        return r.json();
      });
    }
    // slojevi prikaza iz podataka; isti objekt slojevi drži i uređivač
    function slozi(d) {
      podaciKarte = d;
      Object.keys(slojevi).forEach(function (k) {
        if (!PODRUCJA[k]) { karta.removeLayer(slojevi[k]); delete slojevi[k]; }
      });
      var objekti = slojeviObjekata(d.objekti || {});
      Object.keys(objekti).forEach(function (k) { slojevi[k] = objekti[k]; });
      var vode = slojeviVodotoka(d.vodotoci || {});
      Object.keys(vode).forEach(function (k) { slojevi[k] = vode[k]; });
      slojevi.letve = slojLetvi(d.letve || []);
    }
    var podaci = dohvati().then(slozi);

    podaci.then(function () {
      poruka.hidden = true;
      primijeniSlojeve(karta, okvir, slojevi);
      // s adrese uređivanja kartu približi uređivač; dva približavanja
      // zaredom Leaflet ne radi, drugo zanemari dok prvo traje
      if (!stanje.uredi) pocetniPogled(karta, stanje, slojevi);
      smanjiIkone(karta, okvir);
      karta.on('zoomend', function () { smanjiIkone(karta, okvir); });
      karta.on('moveend', zapisi);
      if (window.kartaUredjivanje && L.PM) {
        urediNaKarti = window.kartaUredjivanje.pokreni({
          okvir: okvir, karta: karta, slojevi: slojevi,
          otvoriPlocu: ploca.otvori,
          // nakon spremanja Karta iznova učita slojeve, popis i brojeve
          spremljeno: function () {
            dohvati().then(function (d) {
              slozi(d);
              primijeniSlojeve(karta, okvir, slojevi);
              nedostaje.osvjezi(d);
              osvjeziBrojeve(okvir, d.brojevi);
            }).catch(function () {
              if (window.showToast) window.showToast('Spremljeno je, ali se karta nije osvježila. Osvježite stranicu.');
            });
          }
        });
      }
      nedostaje = pogledNedostaje(okvir, karta, podaciKarte, zapisi, slojevi);
      if (stanje.nedostaje) nedostaje.otvori(stanje.nedostaje, stanje.popis);
      if (stanje.uredi) urediSAdrese(stanje.uredi, podaciKarte, nedostaje);
      zapisi();
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
      okvir.querySelectorAll('input[data-sloj]').forEach(function (s) { s.checked = s.dataset.zadano !== 'false'; });
      primijeniSlojeve(karta, okvir, slojevi);
      zapisi();
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    if (typeof L === 'undefined') return;
    document.querySelectorAll('.karta-stranica').forEach(pokreni);
  });
})();
