// Ravnalo na svim kartama: udaljenost točku po točku, površina lika i
// duljina po nasipu ili vodi između dviju točaka, s km stacionaže kad ih
// crta ima. Ništa se ne sprema. Gumb je gore desno uz puni zaslon i
// položaj (app.js, dodajKontroleKarte); dok se crta na Karti, ravnalo ne
// radi, jer uređivač ima svoju mjeru.
(function () {
  var PLAVA = '#1d4ed8';
  var IKONA = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 16 16 3l5 5L8 21Z"/><path d="m7.5 11.5 2 2M10.5 8.5l2 2M13.5 5.5l2 2"/></svg>';
  var NACINI = [['udaljenost', 'Udaljenost'], ['povrsina', 'Površina'], ['nasip', 'Po nasipu']];

  function M() { return window.mjera; }

  function dijelova(n) {
    var j = n % 10, d = n % 100;
    if (j === 1 && d !== 11) return n + ' dio';
    return n + (j >= 2 && j <= 4 && (d < 12 || d > 14) ? ' dijela' : ' dijelova');
  }

  function stvori(tag, klasa, roditelj, tekst) {
    var e = L.DomUtil.create(tag, klasa || '', roditelj);
    if (tekst != null) e.textContent = tekst;
    if (tag === 'button') e.type = 'button';
    return e;
  }

  function natpis(tekst, klasa) {
    return L.divIcon({ className: 'ravnalo-natpis-omot', iconSize: null, html: '<span class="ravnalo-natpis ' + (klasa || '') + '">' + escapeHtml(tekst) + '</span>' });
  }

  // sredina dviju točaka
  function sredina(a, b) { return L.latLng((a.lat + b.lat) / 2, (a.lng + b.lng) / 2); }

  // dijelovi crte ili ruba sloja kao nizovi točaka; MultiLineString i
  // površine s rupama ugnijezde nizove dublje
  function dijeloviSloja(l) {
    var out = [];
    (function silazi(t) {
      if (!t || !t.length) return;
      if (Array.isArray(t[0])) t.forEach(silazi);
      else out.push(t);
    })(l.getLatLngs());
    return out;
  }

  // najbliža točka na nekom dijelu crte, u točkama zaslona
  function naDijelu(karta, dio, p) {
    var naj = null;
    for (var i = 0; i + 1 < dio.length; i++) {
      var a = karta.latLngToContainerPoint(dio[i]), b = karta.latLngToContainerPoint(dio[i + 1]);
      var dx = b.x - a.x, dy = b.y - a.y, d2 = dx * dx + dy * dy;
      var u = d2 ? Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / d2)) : 0;
      var q = L.point(a.x + u * dx, a.y + u * dy), r = q.distanceTo(p);
      if (!naj || r < naj.r) naj = { r: r, ll: karta.containerPointToLatLng(q) };
    }
    return naj;
  }

  // točke km na dijelu crte, za km uzduž nje: nasip ima km (0+700), voda rkm
  function stacionaza(zbirka, dio) {
    var tocke = [], oznaka = 'km';
    (zbirka.features || []).forEach(function (f) {
      var p = f.properties || {}, g = f.geometry || {};
      if (p.tip !== 'km' && p.tip !== 'rkm') return;
      var c = g.type === 'Point' ? g.coordinates : g.type === 'MultiPoint' ? g.coordinates[0] : null;
      var km = p.tip === 'rkm' ? +p.rkm : M().citajKm(p.km);
      if (!c || km == null || !isFinite(km)) return;
      var m = M().naCrti(dio, { lat: c[1], lng: c[0] });
      if (m && m.udaljenost < 50) tocke.push({ s: m.s, km: km });
      var o = /^(rkm|pkm|kkm|nkm)\b/.exec(p.oznaka || '');
      if (p.tip === 'rkm') oznaka = o ? o[1] : 'rkm';
    });
    tocke.sort(function (a, b) { return a.s - b.s; });
    if (tocke.length < 2) return null;
    return {
      oznaka: oznaka,
      km: function (s) {
        var i = 0;
        while (i < tocke.length - 2 && s > tocke[i + 1].s) i++;
        var a = tocke[i], b = tocke[i + 1], u = b.s === a.s ? 0 : (s - a.s) / (b.s - a.s);
        return a.km + u * (b.km - a.km);
      }
    };
  }

  // Dijelovi nasipa ili toka u jedan niz od kraja do kraja: od svakog
  // mogućeg početka dalje ide najbliži kraj nekog dijela, a pobjeđuje niz s
  // najmanje praznina. Praznina ulazi u duljinu, kao i u stacionažu.
  function uNiz(dijelovi) {
    if (dijelovi.length < 2) return dijelovi[0] || [];
    var d = function (a, b) { return M().udaljenost(a, b); }, najbolji = null;
    dijelovi.forEach(function (prvi, p) {
      [false, true].forEach(function (obrnut) {
        var ostali = dijelovi.filter(function (x, i) { return i !== p; }), niz = obrnut ? prvi.slice().reverse() : prvi.slice(), praznine = 0;
        while (ostali.length) {
          var kraj = niz[niz.length - 1], bi = 0, bo = false, bd = Infinity;
          ostali.forEach(function (o, i) {
            var a = d(kraj, o[0]), b = d(kraj, o[o.length - 1]);
            if (a < bd) { bd = a; bi = i; bo = false; }
            if (b < bd) { bd = b; bi = i; bo = true; }
          });
          var s = ostali.splice(bi, 1)[0];
          niz = niz.concat(bo ? s.slice().reverse() : s);
          praznine += bd;
        }
        if (!najbolji || praznine < najbolji.praznine - 0.01) najbolji = { niz: niz, praznine: praznine };
      });
    });
    return najbolji.niz;
  }

  // crte toka ili nasipa iz zbirke, kao nizovi {lat, lng}
  function crteZbirke(zbirka) {
    var out = [];
    (zbirka.features || []).forEach(function (f) {
      var g = f.geometry || {};
      var nizovi = g.type === 'LineString' ? [g.coordinates] : g.type === 'MultiLineString' ? g.coordinates : [];
      nizovi.forEach(function (n) { if (n.length >= 2) out.push(n.map(function (c) { return L.latLng(c[1], c[0]); })); });
    });
    return out;
  }

  function napravi(karta, gumb) {
    var nacin = 'udaljenost', tocke = [], gotovo = false, nasip = null;
    var sloj = null, kursorCrta = null, kursorNatpis = null, kartica = null, polja = {};
    var dodir = window.matchMedia && window.matchMedia('(pointer: coarse)').matches;
    var zumDvoklikom = false, krizic = null;

    function nas(l) { return !!(l && l._ravnalo); }
    function dodaj(l) { l._ravnalo = true; return l.addTo(sloj); }

    // klik se lijepi na letve, objekte i crte do 12 točaka zaslona
    function prilijepi(ll, p) {
      var naj = null;
      karta.eachLayer(function (l) {
        if (nas(l) || l._ravnaloIgnoriraj) return;
        if ((l instanceof L.Marker || l instanceof L.CircleMarker) && l.getLatLng) {
          var r = karta.latLngToContainerPoint(l.getLatLng()).distanceTo(p);
          if (r <= 12 && (!naj || r < naj.r)) naj = { r: r, ll: l.getLatLng() };
        } else if (l instanceof L.Polyline && l.getBounds && karta.getBounds().intersects(l.getBounds())) {
          dijeloviSloja(l).forEach(function (dio) {
            dio.forEach(function (v) {
              var r = karta.latLngToContainerPoint(v).distanceTo(p);
              if (r <= 12 && (!naj || r < naj.r - 3)) naj = { r: r, ll: v };
            });
            var q = naDijelu(karta, dio, p);
            if (q && q.r <= 8 && (!naj || q.r < naj.r - 3)) naj = q;
          });
        }
      });
      return naj ? L.latLng(naj.ll) : ll;
    }

    // crta nasipa ili vode najbliža kliku, do 15 točaka zaslona; nasip ima
    // prednost, jer često leži tik uz vodu koju brani
    function crtaBlizu(p) {
      var naj = null;
      karta.eachLayer(function (l) {
        var f = l.feature && l.feature.properties;
        if (nas(l) || !f || !(l instanceof L.Polyline) || l instanceof L.Polygon || !(f.kod || f.id)) return;
        if (!karta.getBounds().intersects(l.getBounds())) return;
        dijeloviSloja(l).forEach(function (dio) {
          var q = naDijelu(karta, dio, p), r = q ? q.r + (f.kod ? 10 : 0) : Infinity;
          if (q && q.r <= 15 && (!naj || r < naj.r)) naj = { r: r, ll: q.ll, dio: dio, sloj: l, svojstva: f };
        });
      });
      return naj;
    }

    function imaCrta() {
      var ima = false;
      karta.eachLayer(function (l) {
        var f = l.feature && l.feature.properties;
        if (f && (f.kod || f.id) && l instanceof L.Polyline && !(l instanceof L.Polygon)) ima = true;
      });
      return ima;
    }

    // puna crta i točke km s poslužitelja (Karta crta pojednostavljeno)
    function dohvatiPunu(n) {
      var f = n.svojstva, adresa = f.kod ? '/karta/vodotok/' + encodeURIComponent(f.kod) : f.id ? '/karta/objekt/' + encodeURIComponent(f.id) : null;
      if (!adresa) return;
      fetch(adresa, { credentials: 'same-origin' }).then(function (r) { return r.ok ? r.json() : null; }).then(function (d) {
        if (!d || !d.geojson || nasip !== n) return;
        var zbirka = JSON.parse(d.geojson), crte = crteZbirke(zbirka), naj = null;
        // nasip u više dijelova mjeri se kao jedan niz; tok je jedan, a od
        // više vodotoka u istom zapisu uzima se najbliži
        (n.voda ? crte : [uNiz(crte)]).forEach(function (c) {
          var m = c.length && M().naCrti(c, n.a.ll);
          if (m && (!naj || m.udaljenost < naj.m.udaljenost)) naj = { c: c, m: m };
        });
        if (!naj || naj.m.udaljenost > 100) return;
        n.crta = naj.c;
        n.st = stacionaza(zbirka, naj.c);
        n.a = { s: naj.m.s, ll: L.latLng(naj.m.ll) };
        if (n.b) { var mb = M().naCrti(n.crta, n.b.ll); n.b = { s: mb.s, ll: L.latLng(mb.ll) }; }
        nacrtaj();
      }).catch(function () {});
    }

    function klikNasip(ll, p) {
      if (!nasip || nasip.b) {
        var c = crtaBlizu(p);
        if (!c) { uputa('Kliknite na nasip ili vodu gdje mjerenje počinje.'); return; }
        var crta = c.svojstva.kod ? c.dio : uNiz(dijeloviSloja(c.sloj)), m = M().naCrti(crta, c.ll);
        nasip = { svojstva: c.svojstva, naziv: c.svojstva.naziv || '', voda: !!c.svojstva.kod, crta: crta, st: null, a: { s: m.s, ll: L.latLng(m.ll) }, b: null };
        dohvatiPunu(nasip);
      } else {
        var mb = M().naCrti(nasip.crta, ll);
        var r = mb && karta.latLngToContainerPoint(mb.ll).distanceTo(p);
        if (!mb || r > 20) { uputa('Druga točka mora biti na istoj crti: ' + nasip.naziv + '.'); return; }
        nasip.b = { s: mb.s, ll: L.latLng(mb.ll) };
      }
      nacrtaj();
    }

    function klik(e) {
      karta.closePopup();
      var p = e.containerPoint || karta.latLngToContainerPoint(e.latlng);
      if (nacin === 'nasip') { klikNasip(e.latlng, p); return; }
      if (gotovo) { tocke = []; gotovo = false; }
      if (nacin === 'povrsina' && tocke.length >= 3 && karta.latLngToContainerPoint(tocke[0]).distanceTo(p) <= 10) {
        gotovo = true;
        nacrtaj();
        return;
      }
      var ll = prilijepi(e.latlng, p), zadnja = tocke[tocke.length - 1];
      // dvoklik pošalje i dva klika; isto mjesto se ne broji dvaput
      if (zadnja && karta.latLngToContainerPoint(zadnja).distanceTo(karta.latLngToContainerPoint(ll)) < 3) return;
      tocke.push(ll);
      nacrtaj();
    }

    function dvoklik(e) {
      L.DomEvent.stop(e);
      if (nacin !== 'nasip' && tocke.length >= (nacin === 'povrsina' ? 3 : 2)) { gotovo = true; nacrtaj(); }
    }

    function pomak(e) {
      if (!kursorCrta) return;
      var zadnja = nacin === 'nasip' ? (nasip && !nasip.b ? nasip.a.ll : null) : (!gotovo ? tocke[tocke.length - 1] : null);
      if (!zadnja) { kursorCrta.setLatLngs([]); kursorNatpis.setOpacity(0); return; }
      var put = [zadnja, e.latlng];
      if (nacin === 'povrsina' && tocke.length >= 2) put.push(tocke[0]);
      kursorCrta.setLatLngs(put);
      var t;
      if (nacin === 'nasip') t = 'Kliknite drugu točku na crti';
      else if (nacin === 'povrsina') t = tocke.length >= 2 ? M().hektari(M().povrsina(tocke.concat([e.latlng]))) : M().metri(M().udaljenost(zadnja, e.latlng));
      else {
        var dio = M().udaljenost(zadnja, e.latlng);
        t = tocke.length > 1 ? M().metri(M().duljina(tocke) + dio) + ' (+' + M().metri(dio) + ')' : M().metri(dio);
      }
      kursorNatpis.setLatLng(e.latlng).setIcon(natpis(t, 'ravnalo-natpis-taman ravnalo-natpis-kursor')).setOpacity(1);
    }

    function makniZadnju() {
      if (nacin === 'nasip') {
        if (nasip && nasip.b) nasip.b = null; else nasip = null;
      } else {
        tocke.pop();
        gotovo = false;
      }
      nacrtaj();
    }

    function ocisti() {
      tocke = [];
      gotovo = false;
      nasip = null;
      nacrtaj();
    }

    function crta(t, opcije) { return dodaj(L.polyline(t, L.extend({ interactive: false, pmIgnore: true, snapIgnore: true }, opcije))); }
    function vrh(ll, prvi) {
      return dodaj(L.circleMarker(ll, { radius: prvi ? 7 : 5, color: PLAVA, weight: 3, fillColor: '#ffffff', fillOpacity: 1, interactive: false, pmIgnore: true, snapIgnore: true }));
    }
    function oznaka(ll, tekst, klasa) { return dodaj(L.marker(ll, { icon: natpis(tekst, klasa), interactive: false, keyboard: false, pmIgnore: true, snapIgnore: true })); }

    function nacrtajNasip() {
      if (!nasip) return;
      var n = nasip, st = n.st, ime = n.voda ? 'po vodi' : 'po nasipu';
      function kmTekst(s) { return st ? st.oznaka + ' ' + M().km3(st.km(s)) : ''; }
      if (n.b) {
        var s1 = Math.min(n.a.s, n.b.s), s2 = Math.max(n.a.s, n.b.s);
        crta(M().dioCrte(n.crta, s1, s2), { color: '#2563eb', weight: 14, opacity: 0.35, lineCap: 'round' });
        crta([n.a.ll, n.b.ll], { color: PLAVA, weight: 2, dashArray: '6 4' });
        oznaka(sredina(n.a.ll, n.b.ll), 'ravno ' + M().metri(M().udaljenost(n.a.ll, n.b.ll)));
        oznaka(n.b.ll, M().metri(s2 - s1) + ' ' + ime, 'ravnalo-natpis-taman ravnalo-natpis-gore');
      }
      vrh(n.a.ll, false);
      oznaka(n.a.ll, 'A' + (st ? ' · ' + kmTekst(n.a.s) : ''), 'ravnalo-natpis-desno');
      if (n.b) {
        vrh(n.b.ll, false);
        oznaka(n.b.ll, 'B' + (st ? ' · ' + kmTekst(n.b.s) : ''), 'ravnalo-natpis-desno');
      }
    }

    function nacrtaj() {
      if (!sloj) return;
      sloj.clearLayers();
      kursorCrta = crta([], { color: PLAVA, weight: 2, dashArray: '4 4' });
      kursorNatpis = oznaka(karta.getCenter(), '', 'ravnalo-natpis-taman');
      kursorNatpis.setOpacity(0);
      if (nacin === 'nasip') nacrtajNasip();
      else {
        if (nacin === 'povrsina' && gotovo && tocke.length >= 3) {
          dodaj(L.polygon(tocke, { color: PLAVA, weight: 3, fillColor: '#2563eb', fillOpacity: 0.2, interactive: false, pmIgnore: true, snapIgnore: true }));
          var g = L.polygon(tocke).getBounds().getCenter();
          oznaka(g, M().hektari(M().povrsina(tocke)), 'ravnalo-natpis-taman');
        } else if (tocke.length >= 2) {
          crta(tocke, { color: '#ffffff', weight: 7, opacity: 0.9 });
          crta(tocke, { color: PLAVA, weight: 3 });
        }
        tocke.forEach(function (t, i) { vrh(t, nacin === 'povrsina' && i === 0 && !gotovo && tocke.length >= 3); });
        if (nacin === 'udaljenost') {
          for (var i = 1; i < tocke.length; i++) oznaka(sredina(tocke[i - 1], tocke[i]), M().metri(M().udaljenost(tocke[i - 1], tocke[i])));
          if (gotovo && tocke.length >= 2) oznaka(tocke[tocke.length - 1], M().metri(M().duljina(tocke)), 'ravnalo-natpis-taman ravnalo-natpis-gore');
        }
      }
      osvjeziKarticu();
    }

    function uputa(t) { if (polja.uputa) polja.uputa.textContent = t; }

    // tekst rezultata, za karticu i za Kopiraj
    function rezultat() {
      if (nacin === 'nasip') {
        if (!nasip || !nasip.b) return null;
        var n = nasip, s1 = Math.min(n.a.s, n.b.s), s2 = Math.max(n.a.s, n.b.s), st = n.st;
        var k1 = st && Math.min(st.km(s1), st.km(s2)), k2 = st && Math.max(st.km(s1), st.km(s2));
        var od = st ? ', od ' + st.oznaka + ' ' + M().km3(k1) + ' do ' + st.oznaka + ' ' + M().km3(k2) : '';
        return { iznos: M().metri(s2 - s1), opis: n.naziv + od + '. Ravno od A do B: ' + M().metri(M().udaljenost(n.a.ll, n.b.ll)) + '.',
          kopija: n.naziv + od + ': ' + M().metri(s2 - s1) + (n.voda ? ' po vodi' : ' po nasipu') + ' (ravno ' + M().metri(M().udaljenost(n.a.ll, n.b.ll)) + ')' };
      }
      if (nacin === 'povrsina') {
        if (tocke.length < 3) return null;
        var ha = M().hektari(M().povrsina(tocke)), op = M().metri(M().opseg(tocke));
        return { iznos: ha, opis: 'Opseg ' + op + '.', kopija: 'Površina ' + ha + ', opseg ' + op };
      }
      if (tocke.length < 2) return null;
      var d = M().metri(M().duljina(tocke)), n2 = tocke.length - 1;
      var opis = dijelova(n2) + (n2 > 1 ? '. Ravno od prve do zadnje točke ' + M().metri(M().udaljenost(tocke[0], tocke[n2])) + '.' : '.');
      return { iznos: d, opis: opis, kopija: 'Udaljenost ' + d + ' (' + dijelova(n2) + ')' };
    }

    function tekstUpute() {
      if (nacin === 'nasip') {
        if (!nasip) return 'Kliknite na nasip ili vodu gdje mjerenje počinje.';
        if (!nasip.b) return 'Kliknite drugu točku na istoj crti.';
        return 'Novi klik počinje novo mjerenje. Esc zatvara ravnalo.';
      }
      if (dodir) return gotovo ? 'Gotovo. „Dodaj točku” počinje novo mjerenje.' : 'Pomaknite kartu da križić bude na točki, pa „Dodaj točku”.';
      if (gotovo) return 'Gotovo. Novi klik počinje novo mjerenje, Esc zatvara ravnalo.';
      if (nacin === 'povrsina') return 'Kliknite rubove područja; lik zatvarate klikom na prvu točku ili dvoklikom.';
      return 'Kliknite točku po točku, dvoklik završava, Esc prekida.';
    }

    function osvjeziKarticu() {
      if (!kartica) return;
      var r = rezultat();
      polja.iznos.textContent = r ? r.iznos : '—';
      polja.opis.textContent = r ? r.opis : '';
      polja.opis.hidden = !r;
      uputa(tekstUpute());
      polja.kopiraj.disabled = !r;
      polja.nacini.forEach(function (b) { b.setAttribute('aria-pressed', b.dataset.nacin === nacin ? 'true' : 'false'); });
      if (polja.dodaj) polja.dodaj.hidden = nacin === 'nasip';
    }

    function kopiraj() {
      var r = rezultat();
      if (!r) return;
      var gotovoT = function () { if (window.showToast) window.showToast('Kopirano: ' + r.kopija + '.'); };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(r.kopija).then(gotovoT, function () { if (window.showToast) window.showToast(r.kopija); });
      } else if (window.showToast) window.showToast(r.kopija);
    }

    var kontrola = L.control({ position: 'topleft' });
    kontrola.onAdd = function () {
      var k = L.DomUtil.create('div', 'ravnalo-kartica');
      k.setAttribute('role', 'dialog');
      k.setAttribute('aria-label', 'Ravnalo');
      var glava = stvori('div', 'ravnalo-glava', k);
      stvori('strong', '', glava, 'Ravnalo');
      var zatvori = stvori('button', 'ravnalo-zatvori', glava);
      zatvori.setAttribute('aria-label', 'Zatvori ravnalo');
      zatvori.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>';
      L.DomEvent.on(zatvori, 'click', iskljuci);
      var nacini = stvori('div', 'ravnalo-nacini', k);
      nacini.setAttribute('role', 'group');
      nacini.setAttribute('aria-label', 'Što mjerite');
      polja.nacini = [];
      NACINI.forEach(function (n) {
        if (n[0] === 'nasip' && !imaCrta()) return;
        var b = stvori('button', 'ravnalo-nacin', nacini, n[1]);
        b.dataset.nacin = n[0];
        L.DomEvent.on(b, 'click', function () { nacin = n[0]; ocisti(); });
        polja.nacini.push(b);
      });
      polja.iznos = stvori('div', 'ravnalo-iznos', k);
      polja.iznos.setAttribute('aria-live', 'polite');
      polja.opis = stvori('p', 'ravnalo-opis', k);
      polja.uputa = stvori('p', 'ravnalo-uputa', k);
      var gumbi = stvori('div', 'ravnalo-gumbi', k);
      if (dodir) {
        polja.dodaj = stvori('button', 'ravnalo-dodaj', gumbi, '+ Dodaj točku');
        L.DomEvent.on(polja.dodaj, 'click', function () {
          var c = karta.getCenter();
          klik({ latlng: c, containerPoint: karta.latLngToContainerPoint(c) });
        });
        var makni = stvori('button', 'ravnalo-gumb-tekst', gumbi, 'Makni zadnju');
        L.DomEvent.on(makni, 'click', makniZadnju);
      }
      polja.kopiraj = stvori('button', 'ravnalo-kopiraj', gumbi, 'Kopiraj');
      L.DomEvent.on(polja.kopiraj, 'click', kopiraj);
      var brisi = stvori('button', 'ravnalo-gumb-tekst', gumbi, 'Očisti');
      L.DomEvent.on(brisi, 'click', ocisti);
      L.DomEvent.disableClickPropagation(k);
      L.DomEvent.disableScrollPropagation(k);
      return k;
    };

    function tipka(e) {
      if (!sloj) return;
      var t = e.target && e.target.tagName;
      if (e.key === 'Escape') { iskljuci(); return; }
      if ((e.key === 'Backspace' || e.key === 'Delete') && t !== 'INPUT' && t !== 'TEXTAREA') {
        e.preventDefault();
        makniZadnju();
      }
    }

    function ukljuci() {
      if (sloj) return;
      if (karta._uredjuje) {
        if (window.showToast) window.showToast('Ravnalo ne radi dok crtate; uređivač sam piše duljinu.');
        return;
      }
      karta.closePopup();
      sloj = L.layerGroup().addTo(karta);
      kartica = kontrola.addTo(karta).getContainer();
      zumDvoklikom = karta.doubleClickZoom && karta.doubleClickZoom.enabled();
      if (zumDvoklikom) karta.doubleClickZoom.disable();
      karta.on('click', klik).on('dblclick', dvoklik).on('mousemove', pomak);
      document.addEventListener('keydown', tipka);
      karta.getContainer().classList.add('ravnalo-mjeri');
      if (dodir) krizic = stvori('div', 'ravnalo-krizic', karta.getContainer());
      gumb.setAttribute('aria-pressed', 'true');
      ocisti();
    }

    function iskljuci() {
      if (!sloj) return;
      karta.off('click', klik).off('dblclick', dvoklik).off('mousemove', pomak);
      document.removeEventListener('keydown', tipka);
      if (zumDvoklikom) karta.doubleClickZoom.enable();
      karta.removeLayer(sloj);
      kontrola.remove();
      if (krizic) { L.DomUtil.remove(krizic); krizic = null; }
      karta.getContainer().classList.remove('ravnalo-mjeri');
      sloj = kartica = kursorCrta = kursorNatpis = null;
      tocke = [];
      nasip = null;
      gotovo = false;
      gumb.setAttribute('aria-pressed', 'false');
    }

    return { ukljuci: ukljuci, iskljuci: iskljuci, radi: function () { return !!sloj; } };
  }

  window.ravnaloKarte = {
    // gumb Ravnalo u skupini kontrola gore desno
    gumb: function (karta, roditelj) {
      var b = L.DomUtil.create('button', 'gocop-karta-kontrola gocop-karta-ravnalo', roditelj);
      b.type = 'button';
      b.innerHTML = IKONA;
      b.title = 'Ravnalo: udaljenost, površina ili duljina po nasipu';
      b.setAttribute('aria-label', 'Ravnalo');
      b.setAttribute('aria-pressed', 'false');
      var r = karta._ravnalo = napravi(karta, b);
      L.DomEvent.on(b, 'click', L.DomEvent.stop).on(b, 'click', function () {
        if (r.radi()) r.iskljuci(); else r.ukljuci();
      });
      return b;
    },
    iskljuci: function (karta) {
      if (karta && karta._ravnalo) karta._ravnalo.iskljuci();
    }
  };
})();
