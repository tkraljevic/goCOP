// Uređivanje na Karti (karta_uredjivanje.go, Odluke (4. 11. 2026.)): objekt,
// vodotok i letva crtaju se, pomiču i ispravljaju samo ovdje; stranice
// objekta i vode samo prikazuju i učitavaju datoteku. Alati su
// Leaflet-Geoman, a spremanje ide istim putem kao učitana datoteka, uz istu
// provjeru. Karta.js zove pokreni() kad učita slojeve; ono vrati uredi(p)
// za prozorčić na karti, popis „Što nedostaje” i adresu #uredi=objekt:id.
//
// Geoman radi u načinu „opt-in” (karta.js): uređuje se samo ono što je ovdje
// učitano za uređivanje, nikad slojevi prikaza.
(function () {
  var NARANCASTA = '#ea580c';
  var CRTE = { NASIP: true, BRANA: true, vodotok: true };
  var S_KM = { NASIP: true, BRANA: true }; // točka na nasipu i brani nosi km

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
    actions: { finish: 'Završi', cancel: 'Odustani', removeLastVertex: 'Makni zadnju točku' },
    buttonTitles: {
      drawMarkerButton: 'Nova točka',
      drawPolyButton: 'Novi poligon',
      drawLineButton: 'Nova crta',
      drawCircleButton: 'Krug',
      drawRectButton: 'Pravokutnik',
      editButton: 'Pomiči točke i vrhove (desni klik briše vrh)',
      dragButton: 'Pomakni cijeli dio',
      cutButton: 'Izreži dio poligona',
      deleteButton: 'Obriši dio',
      drawCircleMarkerButton: 'Kružna oznaka',
      snappingButton: 'Prianjanje',
      pinningButton: 'Spoji vrhove',
      rotateButton: 'Okreni',
      drawTextButton: 'Tekst'
    }
  };

  function km3(km) {
    var w = Math.floor(km + 1e-9), m = Math.round((km - w) * 1000);
    if (m === 1000) { w++; m = 0; }
    return w + '+' + ('00' + m).slice(-3);
  }

  function citajKm(t) {
    var m = /^\s*(\d+)\s*\+\s*(\d{1,3})\s*$/.exec(t || '');
    if (m) return parseInt(m[1], 10) + parseInt(m[2], 10) / Math.pow(10, m[2].length);
    var x = parseFloat(String(t || '').replace(',', '.'));
    return isFinite(x) ? x : null;
  }

  // točka na dijelu crte dKm od njegova početka
  function naDijelu(dio, dKm) {
    var mjera = window.mjeraKarte, pre = dKm * 1000;
    for (var i = 0; i + 1 < dio.length; i++) {
      var s = mjera.udaljenost(dio[i], dio[i + 1]);
      if (pre <= s) {
        var u = s ? pre / s : 0;
        return L.latLng(dio[i].lat + u * (dio[i + 1].lat - dio[i].lat), dio[i].lng + u * (dio[i + 1].lng - dio[i].lng));
      }
      pre -= s;
    }
    return dio[dio.length - 1];
  }

  // slozi dijelove nasipa ili toka u jedan niz od kraja do kraja: od svakog
  // mogućeg početka dalje ide najbliži kraj nekog dijela, a dio se okrene ako
  // je nacrtan obrnuto. Pobjeđuje niz s najmanje praznina; za jednu crtu to
  // je ona sama, u smjeru u kojem je nacrtana.
  function sloziNiz(dijelovi) {
    var d = window.mjeraKarte.udaljenost, najbolji = null;
    for (var p = 0; p < dijelovi.length; p++) {
      for (var obrnut = 0; obrnut < 2; obrnut++) {
        var ostali = dijelovi.slice(), prvi = ostali.splice(p, 1)[0];
        var niz = [obrnut ? prvi.slice().reverse() : prvi], praznine = [];
        while (ostali.length) {
          var zadnji = niz[niz.length - 1], kraj = zadnji[zadnji.length - 1], bi = 0, bo = false, bd = Infinity;
          for (var i = 0; i < ostali.length; i++) {
            var a = d(kraj, ostali[i][0]), b = d(kraj, ostali[i][ostali[i].length - 1]);
            if (a < bd) { bd = a; bi = i; bo = false; }
            if (b < bd) { bd = b; bi = i; bo = true; }
          }
          var s = ostali.splice(bi, 1)[0];
          niz.push(bo ? s.slice().reverse() : s);
          praznine.push(bd);
        }
        var ukupno = praznine.reduce(function (z, x) { return z + x; }, 0);
        if (!najbolji || ukupno < najbolji.praznine - 0.01) najbolji = { niz: niz, praznine: ukupno, poPrazninama: praznine };
      }
    }
    return najbolji;
  }

  function obrniNiz(niz) {
    return niz.slice().reverse().map(function (dio) { return dio.slice().reverse(); });
  }

  // duljina niza u km: dijelovi, i praznine kad ulaze u stacionažu
  function duljinaNiza(niz, sPrazninama) {
    var mjera = window.mjeraKarte, m = 0;
    niz.forEach(function (dio, i) {
      if (i > 0 && sPrazninama) m += mjera.udaljenost(niz[i - 1][niz[i - 1].length - 1], dio[0]);
      m += mjera.duljina(dio);
    });
    return m / 1000;
  }

  // km početka i kraja svakog dijela niza, kad stacionaža teče kroz cijeli
  // niz od početnog km. rastegni je omjer službene i nacrtane duljine (1 kad
  // se ne usklađuje).
  function rasponiNiza(niz, od, rastegni, sPrazninama) {
    var mjera = window.mjeraKarte, pre = 0;
    return niz.map(function (dio, i) {
      if (i > 0 && sPrazninama) pre += mjera.udaljenost(niz[i - 1][niz[i - 1].length - 1], dio[0]) / 1000;
      var duz = mjera.duljina(dio) / 1000, r = [od + pre * rastegni, od + (pre + duz) * rastegni];
      pre += duz;
      return r;
    });
  }

  // točke km uzduž niza: oba kraja svakog dijela i svaki puni km na
  // nacrtanom dijelu (u praznini točke nema); dio se rastegne na svoj raspon
  // km. Gdje se dva dijela dodiruju s istim km, ostaje jedna točka.
  function tockeKm(niz, rasponi) {
    var mjera = window.mjeraKarte, out = [];
    niz.forEach(function (dio, i) {
      var kmOd = rasponi[i][0], kmDo = rasponi[i][1], duz = mjera.duljina(dio) / 1000;
      var poKm = duz > 0 ? (kmDo - kmOd) / duz : 1, prije = out[out.length - 1];
      if (!prije || Math.abs(prije.km - kmOd) > 0.0005 || mjera.udaljenost(prije.ll, dio[0]) > 1) out.push({ km: kmOd, ll: dio[0] });
      for (var k = Math.floor(kmOd + 1e-9) + 1; k < kmDo - 0.05; k++) {
        if (k - kmOd >= 0.05) out.push({ km: k, ll: naDijelu(dio, (k - kmOd) / poKm) });
      }
      out.push({ km: kmDo, ll: dio[dio.length - 1] });
    });
    return out;
  }

  // „u 2 dijela”, „u 5 dijelova”
  function uDijelova(n) {
    var j = n % 10, d = n % 100;
    return 'u ' + n + (j >= 2 && j <= 4 && (d < 12 || d > 14) ? ' dijela' : ' dijelova');
  }

  function ikonaKm(km) {
    return L.divIcon({ className: 'karta-km-oznaka', html: '<span>' + escapeHtml(km) + '</span>', iconSize: null, iconAnchor: [0, 0] });
  }

  function jeTockaKm(p) { return !!p && (p.tip === 'km' || p.tip === 'rkm'); }

  window.kartaUredjivanje = {
    natpisi: HR,
    km3: km3,
    citajKm: citajKm,
    tockeKm: tockeKm,
    rasponiNiza: rasponiNiza,
    sloziNiz: sloziNiz,
    pokreni: function (k) {
      var okvir = k.okvir, karta = k.karta;
      var pogled = okvir.querySelector('.karta-pogled-ucrtavanje');
      if (!pogled || !karta.pm) return null;
      var el = function (s) { return pogled.querySelector(s); };
      var aktivno = null, grupa = null, skriveni = [], prethodni = null, oznakeKrajeva = null, novaStacionaza = false, letva = null;

      karta.pm.setLang('hr', HR, 'en');

      // Koraci natrag i naprijed: snimka crteža (GeoJSON) nakon svake
      // izmjene, do 50 koraka. Brišu se kad se ucrtavanje završi.
      var povijest = { natrag: [], naprijed: [], sada: null, vracam: false, cekam: null };

      function snimka() { return JSON.stringify(geojsonGrupe()); }

      function osvjeziKorake() {
        var tb = karta.pm.Toolbar;
        if (!tb.buttons.ponisti) return;
        tb.setButtonDisabled('ponisti', !povijest.natrag.length);
        tb.setButtonDisabled('ponovi', !povijest.naprijed.length);
      }

      function pocniPovijest() {
        clearTimeout(povijest.cekam);
        povijest.natrag = [];
        povijest.naprijed = [];
        povijest.sada = grupa ? snimka() : null;
        osvjeziKorake();
      }

      // jedna radnja pošalje više događaja; snimka ide jednom, kad se smire
      function zabiljezi() {
        if (!aktivno || !grupa || povijest.vracam) return;
        clearTimeout(povijest.cekam);
        povijest.cekam = setTimeout(function () {
          if (!grupa) return;
          var s = snimka();
          if (s === povijest.sada) return;
          if (povijest.sada !== null) povijest.natrag.push(povijest.sada);
          if (povijest.natrag.length > 50) povijest.natrag.shift();
          povijest.naprijed = [];
          povijest.sada = s;
          osvjeziKorake();
        }, 0);
      }

      function vrati(iz, u) {
        if (!iz.length || !grupa) return;
        clearTimeout(povijest.cekam);
        u.push(povijest.sada);
        povijest.sada = iz.pop();
        povijest.vracam = true;
        var pm = karta.pm, nacin = { uredi: pm.globalEditModeEnabled(), pomakni: pm.globalDragModeEnabled(), brisi: pm.globalRemovalModeEnabled() };
        pm.disableGlobalEditMode();
        pm.disableGlobalDragMode();
        pm.disableGlobalRemovalMode();
        grupa.clearLayers();
        slojZaUredjivanje(JSON.parse(povijest.sada)).eachLayer(function (l) { grupa.addLayer(l); });
        if (nacin.uredi) pm.enableGlobalEditMode();
        if (nacin.pomakni) pm.enableGlobalDragMode();
        if (nacin.brisi) pm.enableGlobalRemovalMode();
        if (pm.Toolbar.buttons.brisiVrhove && pm.Toolbar.buttons.brisiVrhove.toggled()) brisiVrhove(true);
        povijest.vracam = false;
        osvjeziMjeru();
        osvjeziKorake();
      }
      function ponisti() { vrati(povijest.natrag, povijest.naprijed); }
      function ponovi() { vrati(povijest.naprijed, povijest.natrag); }

      // Briši vrhove: dok je uključeno, dodir ili klik na vrh ga briše (na
      // tabletu desnog klika nema); inače vrh briše desni klik. Uključuje se
      // sloj po sloj, jer bi Geomanov globalni način uređivanja isključio
      // ovaj gumb kao da je pritisnut gumb „Pomiči točke i vrhove”.
      function brisiVrhove(da) {
        slojeviGrupe(function (l) { return l instanceof L.Polyline && l.pm; }).forEach(function (l) {
          if (da) l.pm.enable({ removeVertexOn: 'click' });
          else { l.pm.disable(); l.pm.setOptions({ removeVertexOn: 'contextmenu' }); }
        });
        okvir.classList.toggle('karta-brise-vrhove', !!da);
      }

      // Prianjanje se pali i gasi gumbom (pamti se na računalu); dok se drži
      // Alt, privremeno je isključeno, i usred crtanja ili povlačenja vrha
      var PRIANJANJE = 'gocop.karta.prianjanje', alt = false;
      function prianjanjeUkljuceno() {
        try { return localStorage.getItem(PRIANJANJE) !== '0'; } catch (e) { return true; }
      }
      function primijeniPrianjanje() {
        karta.pm.setGlobalOptions({ snappable: prianjanjeUkljuceno() && !alt });
      }
      function prianjanje(da) {
        try { localStorage.setItem(PRIANJANJE, da ? '1' : '0'); } catch (e) { /* samo za ovaj pogled */ }
        primijeniPrianjanje();
      }
      document.addEventListener('keydown', function (e) {
        if (e.key === 'Alt' && aktivno && !alt) { alt = true; primijeniPrianjanje(); }
      });
      document.addEventListener('keyup', function (e) {
        if (e.key === 'Alt' && alt) { alt = false; if (aktivno) primijeniPrianjanje(); }
      });
      window.addEventListener('blur', function () { if (alt) { alt = false; if (aktivno) primijeniPrianjanje(); } });

      function izvezi(vrsta) {
        var gj = geojsonGrupe();
        karta.pm.Toolbar.toggleButton('izvezi', false);
        if (!gj.features.length) { greska('Na karti nema ničega za izvoz.'); return; }
        window.datotekaKarte.preuzmi(gj, aktivno.naziv, vrsta);
      }

      // Učitaj, Izvezi, Briši vrhove, Poništi i Ponovi među Geomanovim
      // alatima; prave se jednom, a pokazuju sa svakim ucrtavanjem
      function alati() {
        var tb = karta.pm.Toolbar;
        if (tb.buttons.ucitaj) return;
        tb.createCustomControl({ name: 'ucitaj', block: 'draw', title: 'Učitaj KML ili GeoJSON', className: 'gocop-pm-ucitaj', toggle: false,
          onClick: function () { el('.karta-ucrtavanje-datoteka').click(); } });
        tb.createCustomControl({ name: 'izvezi', block: 'draw', title: 'Izvezi crtež kao GeoJSON ili KML', className: 'gocop-pm-izvezi', toggle: true,
          actions: [{ name: 'izvezi-geojson', text: 'GeoJSON', onClick: function () { izvezi('geojson'); } },
            { name: 'izvezi-kml', text: 'KML (Google Earth)', onClick: function () { izvezi('kml'); } }, 'cancel'] });
        // Poništi i Ponovi ga ne gase; gasi ga drugi način (uređivanje,
        // pomicanje, brisanje dijela, crtanje), vidi ugasiBrisanjeVrhova
        tb.createCustomControl({ name: 'brisiVrhove', block: 'edit', title: 'Briši vrhove: dodir ili klik na vrh ga briše', className: 'gocop-pm-brisi-vrh', toggle: true,
          disableByOtherButtons: false,
          actions: [{ name: 'brisi-uputa', text: 'Dodirnite ili kliknite vrh da ga obrišete', onClick: function () {} },
            { name: 'brisi-gotovo', text: 'Gotovo', onClick: function () { tb.toggleButton('brisiVrhove', false); brisiVrhove(false); } }],
          afterClick: function (e, ctx) { brisiVrhove(ctx.button.toggled()); } });
        tb.createCustomControl({ name: 'prianjanje', block: 'options', title: 'Prianjanje na susjedne crte i točke (Alt ga privremeno isključi)',
          className: 'gocop-pm-prianjanje', toggle: true, disableOtherButtons: false, disableByOtherButtons: false,
          afterClick: function (e, ctx) { prianjanje(ctx.button.toggled()); } });
        tb.createCustomControl({ name: 'ponisti', block: 'custom', title: 'Poništi zadnju izmjenu (Ctrl+Z)', className: 'gocop-pm-ponisti',
          toggle: false, disableOtherButtons: false, onClick: ponisti });
        tb.createCustomControl({ name: 'ponovi', block: 'custom', title: 'Ponovi (Ctrl+Shift+Z)', className: 'gocop-pm-ponovi',
          toggle: false, disableOtherButtons: false, onClick: ponovi });
      }

      function ugasiBrisanjeVrhova(e) {
        var b = karta.pm.Toolbar.buttons.brisiVrhove;
        if (!b || !b.toggled() || (e && e.enabled === false)) return;
        karta.pm.Toolbar.toggleButton('brisiVrhove', false, false);
        brisiVrhove(false);
      }
      karta.on('pm:globaleditmodetoggled pm:globaldragmodetoggled pm:globalremovalmodetoggled pm:drawstart', ugasiBrisanjeVrhova);

      document.addEventListener('keydown', function (e) {
        if (!aktivno || !grupa || !(e.ctrlKey || e.metaKey)) return;
        var t = e.target && e.target.tagName;
        if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT') return;
        var tipka = (e.key || '').toLowerCase();
        if (tipka === 'z' && !e.shiftKey) { e.preventDefault(); ponisti(); }
        else if ((tipka === 'z' && e.shiftKey) || tipka === 'y') { e.preventDefault(); ponovi(); }
      });

      // Nova crta koja počne ili završi na kraju postojeće spaja se s njom u
      // jednu crtu, umjesto novog dijela. Kraj se broji do 8 točaka zaslona
      // (najmanje 5 m), jer prianjanje zna sjesti na susjednu crtu tik uz
      // vrh; spojena crta preuzme točan kraj postojeće, bez praznine.
      function nastavi(nova) {
        var t = nova.getLatLngs(), d = window.mjera.udaljenost, spojeno = false;
        if (t.length < 2) return false;
        var prag = Math.max(5, d(karta.containerPointToLatLng([0, 0]), karta.containerPointToLatLng([8, 0])));
        var prvi = t[0], zadnji = t[t.length - 1], obrnuto = t.slice().reverse();
        crteGrupe().forEach(function (l) {
          if (spojeno || l === nova) return;
          var lt = l.getLatLngs(), dij = lt.length && Array.isArray(lt[0]) ? lt.slice() : [lt];
          dij.forEach(function (dio, i) {
            if (spojeno || dio.length < 2) return;
            var pocetak = dio[0], kraj = dio[dio.length - 1], novi = null;
            if (d(prvi, kraj) < prag) novi = dio.concat(t.slice(1));
            else if (d(zadnji, pocetak) < prag) novi = t.slice(0, -1).concat(dio);
            else if (d(prvi, pocetak) < prag) novi = obrnuto.slice(0, -1).concat(dio);
            else if (d(zadnji, kraj) < prag) novi = dio.concat(obrnuto.slice(1));
            if (!novi) return;
            dij[i] = novi;
            l.setLatLngs(dij.length === 1 ? dij[0] : dij);
            spojeno = true;
          });
        });
        if (!spojeno) return false;
        grupa.removeLayer(nova);
        if (window.showToast) window.showToast('Crta je nastavljena (+' + window.mjera.metri(window.mjera.duljina(t)) + '). Ctrl+Z poništava.');
        return true;
      }

      // Preklapanje s drugim nasipom: točke crte svakih 10 m bliže od 10 m
      // nekom drugom nasipu, u komadu duljem od 50 m. Kraj jednog nasipa
      // treba biti početak drugog (Draž i usporni nasip Karašice, 4. 11.).
      var preklapanje = { cekam: null, sloj: null, tocke: null };

      function uzorci(dio, korak) {
        var out = [dio[0]], ostatak = korak, M = window.mjera;
        for (var i = 0; i + 1 < dio.length; i++) {
          var a = dio[i], b = dio[i + 1], duz = M.udaljenost(a, b), pre = 0;
          while (duz - pre >= ostatak) {
            pre += ostatak;
            var u = pre / duz;
            out.push(L.latLng(a.lat + u * (b.lat - a.lat), a.lng + u * (b.lng - a.lng)));
            ostatak = korak;
          }
          ostatak -= duz - pre;
        }
        return out;
      }

      function drugiNasipi(granice) {
        var out = [];
        Object.keys(k.slojevi).forEach(function (kljuc) {
          var s = k.slojevi[kljuc];
          if (!s || !s.eachLayer) return;
          s.eachLayer(function (l) {
            var p = l.feature && l.feature.properties;
            if (!p || p.vrsta !== 'NASIP' || p.adresa === aktivno.adresa || !(l instanceof L.Polyline)) return;
            if (!l.getBounds().pad(0.05).intersects(granice)) return;
            var t = l.getLatLngs();
            out.push({ naziv: p.naziv, crte: t.length && Array.isArray(t[0]) ? t : [t] });
          });
        });
        return out;
      }

      function nadjiPreklapanje() {
        var dij = dijeloviCrta(), M = window.mjera, nalaz = null;
        if (!dij.length) return null;
        var granice = L.latLngBounds(dij[0]);
        dij.forEach(function (dio) { granice.extend(L.latLngBounds(dio)); });
        drugiNasipi(granice).forEach(function (o) {
          var naj = [], niz = [];
          dij.forEach(function (dio) {
            uzorci(dio, 10).forEach(function (u) {
              var blizu = o.crte.some(function (c) { var m = M.naCrti(c, u); return m && m.udaljenost < 10; });
              if (blizu) niz.push(u);
              else { if (niz.length > naj.length) naj = niz; niz = []; }
            });
            if (niz.length > naj.length) naj = niz;
            niz = [];
          });
          var duz = (naj.length - 1) * 10;
          if (duz >= 50 && (!nalaz || duz > nalaz.duz)) nalaz = { naziv: o.naziv, duz: duz, tocke: naj };
        });
        return nalaz;
      }

      function makniOznakuPreklapanja() {
        if (preklapanje.sloj) { karta.removeLayer(preklapanje.sloj); preklapanje.sloj = null; }
      }

      function provjeriPreklapanje() {
        clearTimeout(preklapanje.cekam);
        preklapanje.cekam = setTimeout(function () {
          var kutija = el('.karta-ucrtavanje-preklapanje');
          makniOznakuPreklapanja();
          var n = aktivno && aktivno.vrstaObjekta === 'NASIP' && grupa ? nadjiPreklapanje() : null;
          preklapanje.tocke = n && n.tocke;
          kutija.hidden = !n;
          if (n) {
            el('.karta-ucrtavanje-preklapanje-tekst').textContent = 'Crta se ' + window.mjera.metri(n.duz) + ' poklapa s nasipom ' + n.naziv +
              '. Kraj jednog nasipa treba biti početak drugog.';
          }
        }, 300);
      }

      function pokaziPreklapanje() {
        if (!preklapanje.tocke) return;
        makniOznakuPreklapanja();
        preklapanje.sloj = L.polyline(preklapanje.tocke, { color: '#f59f00', weight: 14, opacity: 0.55, interactive: false, pmIgnore: true, snapIgnore: true }).addTo(karta);
        prikazi(preklapanje.sloj.getBounds().pad(0.5));
      }

      function vidljivPogled() {
        return Array.prototype.filter.call(okvir.querySelectorAll('.karta-pogled-slojevi, .karta-pogled-nedostaje'), function (p) { return !p.hidden; })[0];
      }

      function slojeviGrupe(test) {
        var out = [];
        if (grupa) grupa.eachLayer(function (l) { if (karta.hasLayer(l) && test(l)) out.push(l); });
        return out;
      }
      function crteGrupe() { return slojeviGrupe(function (l) { return l instanceof L.Polyline && !(l instanceof L.Polygon); }); }
      function plosneGrupe() { return slojeviGrupe(function (l) { return l instanceof L.Polygon; }); }

      // dijelovi svih crta: MultiLineString daje po jedan dio za svaki komad
      function dijeloviCrta() {
        var out = [];
        crteGrupe().forEach(function (l) {
          var t = l.getLatLngs();
          (t.length && Array.isArray(t[0]) ? t : [t]).forEach(function (d) { if (d.length >= 2) out.push(d); });
        });
        return out;
      }

      function osvjeziMjeru() {
        if (!aktivno) return;
        var mjera = window.mjeraKarte, iznos = el('.karta-ucrtavanje-iznos');
        var traka = el('.karta-ucrtavanje-traka'), dionica = el('.karta-ucrtavanje-dionica'), st = el('.karta-ucrtavanje-stacionaza');
        el('.karta-ucrtavanje-oznaka').textContent = aktivno.vrsta === 'letva' ? 'Položaj' : 'Nacrtano';
        if (aktivno.vrsta === 'letva') {
          var ll = letva && letva.getLatLng();
          iznos.textContent = ll ? ll.lat.toFixed(5).replace('.', ',') + ' N, ' + ll.lng.toFixed(5).replace('.', ',') + ' E' : 'još nije postavljen';
          traka.hidden = dionica.hidden = st.hidden = el('.karta-ucrtavanje-prekidac').hidden = el('.karta-ucrtavanje-dijelovi').hidden =
            el('.karta-ucrtavanje-prekini').hidden = true;
          return;
        }
        var crte = crteGrupe(), plosne = plosneGrupe(), dij = dijeloviCrta(), m = 0, tocaka = 0;
        dij.forEach(function (d) { m += mjera.duljina(d); tocaka += d.length; });
        var niz = dij.length > 1 ? sloziNiz(dij) : null;
        // crta nasipa ili toka obrisana, a točke km ostale: tako se ne sprema
        var bezCrte = !!CRTE[aktivno.vrstaObjekta] && !crte.length && slojeviGrupe(function () { return true; }).length > 0;
        if (crte.length) iznos.textContent = mjera.metri(m);
        else if (plosne.length) iznos.textContent = mjera.hektari(plosne.reduce(function (z, l) { return z + mjera.povrsina(l.getLatLngs()[0]); }, 0));
        else if (bezCrte) iznos.textContent = 'crte nema';
        else iznos.textContent = slojeviGrupe(function () { return true; }).length ? 'točka' : 'još ništa';
        el('.karta-ucrtavanje-mjera').classList.toggle('karta-ucrtavanje-bez-crte', bezCrte);
        el('.karta-ucrtavanje-bezcrte').hidden = !bezCrte;
        el('.karta-ucrtavanje-spremi').disabled = bezCrte;
        provjeriPreklapanje();
        var sl = aktivno.duljina_km;
        traka.hidden = !(sl > 0 && crte.length);
        dionica.hidden = !(sl > 0);
        if (sl > 0) {
          var km = m / 1000, ostatak = sl - km;
          traka.firstElementChild.style.width = Math.min(100, km / sl * 100) + '%';
          dionica.textContent = 'Po dionici ' + mjera.metri(sl * 1000) + (aktivno.gdje ? ' (' + aktivno.gdje + ')' : '') + '. ' +
            (!crte.length ? '' : Math.abs(ostatak) < 0.01 ? 'Duljina se slaže. '
              : ostatak > 0 ? 'Do kraja po stacionaži još ' + mjera.metri(ostatak * 1000) + '. ' : 'Crta je dulja za ' + mjera.metri(-ostatak * 1000) + '. ') +
            (crte.length ? 'Točaka: ' + tocaka + '.' : '');
        }
        var opisDijelova = el('.karta-ucrtavanje-dijelovi');
        opisDijelova.hidden = !niz;
        if (niz) opisDijelova.textContent = 'Crta je ' + uDijelova(dij.length) + ', praznine između njih ukupno ' + mjera.metri(niz.praznine) + '.';
        el('.karta-stacionaza-praznine-redak').hidden = !niz;
        // km po dionicama ne trebaju početni km, rastezanje ni praznine
        var poDij = poDionicama(dij);
        el('.karta-stacionaza-po-dionicama-redak').hidden = !poDij;
        pogled.querySelectorAll('.karta-stacionaza-od, .karta-stacionaza-uskladi, .karta-stacionaza-praznine').forEach(function (x) {
          x.disabled = poDij && el('.karta-stacionaza-po-dionicama').checked;
        });
        st.hidden = !(CRTE[aktivno.vrstaObjekta] && crte.length && !aktivno.stacionazaEnc);
        el('.karta-ucrtavanje-prekini').hidden = !crte.length;
        el('.karta-ucrtavanje-prekidac').hidden = !slojeviGrupe(function (l) { return jeTockaKm(l.feature && l.feature.properties); }).length;
        el('.karta-stacionaza-uskladi-redak').hidden = !(sl > 0);
        oznaciKrajeve(dij);
      }

      // za generiranje stacionaže vidi se koji je kraj A, a koji B
      function oznaciKrajeve(dij) {
        if (oznakeKrajeva) { karta.removeLayer(oznakeKrajeva); oznakeKrajeva = null; }
        var st = el('.karta-ucrtavanje-stacionaza');
        if (!dij.length || st.hidden || !st.open) return;
        var niz = sloziNiz(dij).niz, prvi = niz[0], zadnji = niz[niz.length - 1];
        var t = [prvi[0], zadnji[zadnji.length - 1]];
        function kraj(ll, boja, natpis) {
          return L.circleMarker(ll, { radius: 8, color: '#ffffff', weight: 2, fillColor: boja, fillOpacity: 1, interactive: false, pmIgnore: true })
            .bindTooltip(natpis, { permanent: true, direction: 'right', className: 'karta-kraj-oznaka' });
        }
        oznakeKrajeva = L.layerGroup([kraj(t[0], '#16a34a', 'A'), kraj(t[t.length - 1], '#dc2626', 'B')], { pmIgnore: true }).addTo(karta);
      }

      // točka km: na nasipu se pomiče i briše kao svaka točka; na toku je
      // samo za pogled, jer spremanje toka bez nove stacionaže drži upisane
      function tockaKm(ll, svojstva) {
        var vodotok = aktivno.vrsta === 'vodotok';
        var m = L.marker(ll, { pmIgnore: vodotok, interactive: !vodotok, icon: ikonaKm(svojstva.km || (svojstva.rkm != null ? km3(+svojstva.rkm) : '')) });
        m.feature = { type: 'Feature', properties: svojstva };
        return m;
      }

      function slojZaUredjivanje(gj) {
        return L.geoJSON(gj, {
          pmIgnore: false,
          style: function () { return { color: NARANCASTA, weight: 4, opacity: 0.95 }; },
          pointToLayer: function (f, ll) {
            return jeTockaKm(f.properties) ? tockaKm(ll, f.properties) : L.marker(ll, { pmIgnore: false });
          }
        });
      }

      function sakrijPrikaz(adresa) {
        skriveni = [];
        if (!adresa) return;
        Object.keys(k.slojevi).forEach(function (kljuc) {
          var s = k.slojevi[kljuc];
          if (!s || !s.eachLayer) return;
          s.eachLayer(function (l) {
            var p = (l.feature && l.feature.properties) || l.podaci || {};
            if (p.adresa !== adresa) return;
            skriveni.push({ sloj: s, l: l });
          });
        });
        skriveni.forEach(function (x) { x.sloj.removeLayer(x.l); });
      }

      function vratiPrikaz() {
        skriveni.forEach(function (x) { x.sloj.addLayer(x.l); });
        skriveni = [];
      }

      function kontrole(vrsta) {
        var crta = !!CRTE[vrsta], povrsina = vrsta === 'povrsina-vode';
        return {
          position: 'topleft', drawMarker: (!crta && !povrsina) || !!S_KM[vrsta], drawPolyline: crta, drawPolygon: !crta,
          drawCircle: false, drawCircleMarker: false, drawRectangle: false, drawText: false,
          editMode: true, dragMode: true, cutPolygon: false, removalMode: true, rotateMode: false,
          ucitaj: true, izvezi: true, brisiVrhove: true, prianjanje: true, ponisti: true, ponovi: true
        };
      }

      // približi crtež, a da ne ostane ispod ploče desno (na računalu)
      function prikazi(g) {
        if (!g || !g.isValid()) return;
        var uski = window.matchMedia('(max-width: 839px)').matches;
        karta.fitBounds(g, { paddingTopLeft: [48, 48], paddingBottomRight: [uski ? 48 : 384 + 48, uski ? 120 : 48], maxZoom: 16 });
      }

      function greska(t) {
        var g = el('.karta-ucrtavanje-greska');
        g.textContent = t || '';
        g.hidden = !t;
      }

      function uputa(a) {
        if (a.vrsta === 'letva') return 'Povucite letvu na pravo mjesto ili kliknite na karti gdje stoji.';
        if (a.vrstaObjekta === 'povrsina-vode') {
          return 'Alatom „Novi poligon” kliknite točku po točku uz obalu, a površinu zatvorite klikom na prvu točku. Vrh pomičete povlačenjem, a brišete desnim klikom ili alatom Briši vrhove. Ctrl+Z poništava.';
        }
        var u = CRTE[a.vrstaObjekta]
          ? 'Alatom „Nova crta” kliknite točku po točku, a zadnju dvaput ili „Završi”. Novu crtu počnite na kraju postojeće i nastavit će je. Vrh pomičete povlačenjem, a brišete desnim klikom ili alatom Briši vrhove. Ctrl+Z poništava.'
          : 'Alatom „Nova točka” postavite objekt, a „Novi poligon” crta površinu. Točku pomičete povlačenjem.';
        if (S_KM[a.vrstaObjekta]) u += ' „Nova točka” pita za km stacionaže.';
        return u;
      }

      function pocni(a, gj) {
        aktivno = a;
        prethodni = vidljivPogled();
        if (prethodni) prethodni.hidden = true;
        pogled.hidden = false;
        okvir.classList.add('karta-u-uredjivanju');
        k.otvoriPlocu();
        karta.closePopup();
        el('.karta-ucrtavanje-naziv').textContent = a.naziv;
        el('.karta-ucrtavanje-uputa').textContent = uputa(a);
        prekidaj(false);
        // dok se crta, ravnalo ne radi (ravnalo-karte.js)
        karta._uredjuje = true;
        if (window.ravnaloKarte) window.ravnaloKarte.iskljuci(karta);
        el('.karta-ucrtavanje-stacionaza').open = false;
        el('.karta-stacionaza-od').value = km3(a.odKm || 0);
        el('.karta-stacionaza-kraj').value = a.vrsta === 'vodotok' ? 'B' : 'A';
        el('.karta-stacionaza-uskladi').checked = a.duljina_km > 0;
        el('.karta-stacionaza-po-dionicama').checked = (a.odsjeci || []).length > 1;
        novaStacionaza = false;
        greska('');
        sakrijPrikaz(a.adresa);
        if (a.vrsta === 'letva') {
          pocniLetvu(gj && gj.ll);
          osvjeziMjeru();
          return;
        }
        grupa = slojZaUredjivanje(gj).addTo(karta);
        karta.pm.setGlobalOptions({ layerGroup: grupa, snappable: true, snapDistance: 15, pathOptions: { color: NARANCASTA, weight: 4 } });
        alati();
        karta.pm.addControls(kontrole(a.vrstaObjekta));
        karta.pm.Toolbar.toggleButton('prianjanje', prianjanjeUkljuceno(), false);
        primijeniPrianjanje();
        if (window.mjeraKarte) window.mjeraKarte.prati(karta, grupa);
        grupa.on('pm:edit pm:dragend pm:markerdragend pm:vertexremoved pm:remove layeradd layerremove', osvjeziMjeru);
        grupa.on('pm:edit pm:dragend pm:markerdragend pm:vertexremoved pm:remove pm:cut layeradd layerremove', zabiljezi);
        pocniPovijest();
        var g = grupa.getBounds();
        prikazi(g.isValid() ? g : a.granice);
        osvjeziMjeru();
      }

      function klikLetve(e) { postaviLetvu(e.latlng); }

      function postaviLetvu(ll) {
        if (letva) letva.setLatLng(ll);
        else {
          letva = L.marker(ll, { draggable: true, pmIgnore: true, zIndexOffset: 1000, icon: stvoriLetvaIkonu() || new L.Icon.Default() }).addTo(karta);
          letva.on('dragend', osvjeziMjeru);
        }
        osvjeziMjeru();
      }

      function pocniLetvu(ll) {
        if (ll) { postaviLetvu(ll); prikazi(L.latLngBounds([ll, ll])); }
        karta.on('click', klikLetve);
        okvir.classList.add('karta-postavlja-letvu');
      }

      function zavrsi() {
        prekidaj(false);
        karta.off('click', klikLetve);
        okvir.classList.remove('karta-u-uredjivanju', 'karta-postavlja-letvu');
        if (letva) { karta.removeLayer(letva); letva = null; }
        if (oznakeKrajeva) { karta.removeLayer(oznakeKrajeva); oznakeKrajeva = null; }
        karta._uredjuje = false;
        makniOznakuPreklapanja();
        el('.karta-ucrtavanje-preklapanje').hidden = true;
        el('.karta-ucrtavanje-spremi').disabled = false;
        if (karta.pm.Toolbar.buttons.brisiVrhove && karta.pm.Toolbar.buttons.brisiVrhove.toggled()) {
          karta.pm.Toolbar.toggleButton('brisiVrhove', false);
          brisiVrhove(false);
        }
        if (grupa) {
          karta.pm.disableDraw();
          karta.pm.disableGlobalEditMode();
          karta.pm.disableGlobalDragMode();
          karta.pm.disableGlobalRemovalMode();
          karta.pm.removeControls();
          karta.removeLayer(grupa);
          grupa = null;
        }
        aktivno = null;
        pocniPovijest();
        pogled.hidden = true;
        if (prethodni) prethodni.hidden = false;
      }

      function odustani() {
        vratiPrikaz();
        zavrsi();
      }

      // Prekid crte: prvi klik kaže gdje prekid počinje, drugi gdje završava.
      // Komad između se makne, a crta ostaje isti zapis u više dijelova
      // (nasip prekinut ušćem spojnog kanala). Dvaput isto mjesto razdvoji
      // crtu bez praznine, na primjer na granici dionica.
      var prekid = null;

      // najbliže mjesto na nekoj crti klikom, do 25 točaka zaslona
      function mjestoNaCrti(ll) {
        var p = karta.latLngToLayerPoint(ll), naj = null;
        crteGrupe().forEach(function (l) {
          var t = l.getLatLngs(), dij = t.length && Array.isArray(t[0]) ? t : [t];
          dij.forEach(function (dio, di) {
            for (var i = 0; i + 1 < dio.length; i++) {
              var a = karta.latLngToLayerPoint(dio[i]), b = karta.latLngToLayerPoint(dio[i + 1]);
              var dx = b.x - a.x, dy = b.y - a.y, d2 = dx * dx + dy * dy;
              var u = d2 ? Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / d2)) : 0;
              var q = L.point(a.x + u * dx, a.y + u * dy), r = q.distanceTo(p);
              if (r <= 25 && (!naj || r < naj.r)) naj = { l: l, dio: di, seg: i, u: u, r: r, ll: karta.layerPointToLatLng(q) };
            }
          });
        });
        return naj;
      }

      function uputaPrekida(t) {
        var u = el('.karta-ucrtavanje-prekid-uputa');
        u.textContent = t || '';
        u.hidden = !t;
      }

      function prekidaj(da) {
        var gumb = el('.karta-ucrtavanje-prekini');
        if (prekid && prekid.oznaka) karta.removeLayer(prekid.oznaka);
        prekid = da ? {} : null;
        gumb.setAttribute('aria-pressed', da ? 'true' : 'false');
        okvir.classList.toggle('karta-prekida-crtu', !!da);
        if (da) {
          karta.pm.disableDraw();
          karta.pm.disableGlobalEditMode();
          karta.pm.disableGlobalDragMode();
          karta.pm.disableGlobalRemovalMode();
          karta.on('click', klikPrekida);
          uputaPrekida('Kliknite na crtu gdje prekid počinje, pa gdje završava. Za razdvajanje bez praznine kliknite dvaput isto mjesto.');
        } else {
          karta.off('click', klikPrekida);
          uputaPrekida('');
        }
      }

      function klikPrekida(e) {
        karta.closePopup(); // klik je prošao i kroz susjedni sloj prikaza
        var m = mjestoNaCrti(e.latlng);
        if (!m) { uputaPrekida('Kliknite bliže crti.'); return; }
        if (!prekid.od) {
          prekid.od = m;
          prekid.oznaka = L.circleMarker(m.ll, { radius: 6, color: '#ffffff', weight: 2, fillColor: '#dc2626', fillOpacity: 1, interactive: false, pmIgnore: true }).addTo(karta);
          uputaPrekida('Sad kliknite gdje prekid završava, ili isto mjesto za razdvajanje bez praznine.');
          return;
        }
        var a = prekid.od, b = m;
        if (a.l !== b.l || a.dio !== b.dio) { uputaPrekida('Kraj prekida mora biti na istom dijelu crte. Kliknite ponovno gdje završava.'); return; }
        if (b.seg + b.u < a.seg + a.u) { var x = a; a = b; b = x; }
        var t = a.l.getLatLngs(), dijelovi = t.length && Array.isArray(t[0]) ? t.slice() : [t];
        var dio = dijelovi[a.dio], bezPraznine = window.mjeraKarte.udaljenost(a.ll, b.ll) < 1;
        var prije = dio.slice(0, a.seg + 1).concat([a.ll]);
        var poslije = [bezPraznine ? a.ll : b.ll].concat(dio.slice(b.seg + 1));
        var novi = [prije, poslije].filter(function (d) { return d.length >= 2 && window.mjeraKarte.duljina(d) > 0.5; });
        dijelovi.splice.apply(dijelovi, [a.dio, 1].concat(novi));
        a.l.setLatLngs(dijelovi.length === 1 ? dijelovi[0] : dijelovi);
        zabiljezi();
        prekidaj(false);
        greska('');
        osvjeziMjeru();
        if (window.showToast) window.showToast(bezPraznine ? 'Crta je razdvojena. Spremite kad je sve kako treba.' : 'Komad crte je maknut (' + window.mjeraKarte.metri(window.mjeraKarte.udaljenost(a.ll, b.ll)) + '). Spremite kad je sve kako treba.');
      }

      // točke km uzduž svih dijelova složenih u niz: početni km na kraju A
      // ili B, po želji rastegnuto na službenu duljinu po dionicama. Crte
      // ostaju kako su nacrtane (tok nizvodno), mijenjaju se samo točke.
      // nasip u više dionica nacrtan u isto toliko dijelova: svaki dio može
      // dobiti km svoje dionice (od najmanjeg km, redom niza)
      function poDionicama(dij) {
        var o = aktivno.odsjeci || [];
        return o.length > 1 && o.length === dij.length;
      }

      function generiraj() {
        var dij = dijeloviCrta();
        if (!dij.length) return;
        var niz = sloziNiz(dij).niz;
        if (el('.karta-stacionaza-kraj').value === 'B') niz = obrniNiz(niz);
        var rasponi;
        if (poDionicama(dij) && el('.karta-stacionaza-po-dionicama').checked) {
          rasponi = aktivno.odsjeci.map(function (o) { return [o.od, o.do]; });
        } else {
          var od = citajKm(el('.karta-stacionaza-od').value);
          if (od === null) { greska('Početni km upišite kao 0+000 ili 12,44.'); return; }
          var sPrazninama = dij.length === 1 || el('.karta-stacionaza-praznine').checked;
          var nacrtano = duljinaNiza(niz, sPrazninama), sl = aktivno.duljina_km;
          var rastegni = el('.karta-stacionaza-uskladi').checked && sl > 0 && nacrtano > 0 ? sl / nacrtano : 1;
          rasponi = rasponiNiza(niz, od, rastegni, sPrazninama);
        }
        slojeviGrupe(function (l) { return jeTockaKm(l.feature && l.feature.properties); }).forEach(function (l) { grupa.removeLayer(l); });
        tockeKm(niz, rasponi).forEach(function (x) {
          var oznaka = km3(x.km);
          grupa.addLayer(tockaKm(x.ll, aktivno.vrsta === 'vodotok'
            ? { tip: 'rkm', rkm: Math.round(x.km * 1000) / 1000, oznaka: oznaka, status: 'kalibrirano' }
            : { tip: 'km', km: oznaka }));
        });
        novaStacionaza = true;
        greska('');
        osvjeziMjeru();
      }

      // datoteka se doda crtežu; sprema se tek gumbom Spremi
      function ucitaj(datoteka) {
        window.datotekaKarte.procitaj(datoteka).then(function (gj) {
          var n = 0;
          slojZaUredjivanje(gj).eachLayer(function (l) { grupa.addLayer(l); n++; });
          if (gj.features.some(function (f) { return jeTockaKm(f.properties); })) novaStacionaza = true;
          greska('');
          prikazi(grupa.getBounds());
          osvjeziMjeru();
          if (window.showToast) window.showToast('Iz „' + datoteka.name + '” dodano: ' + n + '. Pregledajte i spremite.');
        }).catch(function (e) { greska(e.message); });
      }

      function geojsonGrupe() {
        var znacajke = [];
        slojeviGrupe(function (l) { return !!l.toGeoJSON; }).forEach(function (l) {
          var f = l.toGeoJSON(), g = f.geometry;
          if (!g || (g.type === 'LineString' && g.coordinates.length < 2)) return;
          if (g.type === 'MultiLineString') {
            g.coordinates = g.coordinates.filter(function (d) { return d.length >= 2; });
            if (!g.coordinates.length) return;
          }
          f.properties = f.properties || {};
          if (!f.properties.tip) {
            if (/LineString$/.test(g.type)) f.properties.tip = aktivno.vrsta === 'vodotok' ? 'vodotok' : aktivno.vrstaObjekta === 'NASIP' ? 'nasip' : 'crta';
            else if (/Polygon$/.test(g.type)) f.properties.tip = 'poligon';
            else f.properties.tip = 'tocka';
          }
          znacajke.push(f);
        });
        return { type: 'FeatureCollection', features: znacajke };
      }

      function posalji(adresa, tijelo) {
        return fetch(adresa, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(tijelo) })
          .then(function (res) {
            if (res.ok) return null;
            return res.text().then(function (t) { throw new Error(t.trim() || ('greška ' + res.status)); });
          }, function () { throw new Error('nema veze s programom.'); });
      }

      function zahtjev(a) {
        if (a.vrsta === 'letva') {
          if (!letva) throw new Error('Postavite letvu na karti.');
          var ll = letva.getLatLng();
          return ['/karta/letva/' + encodeURIComponent(a.id) + '/polozaj', { lat: ll.lat, lon: ll.lng }];
        }
        var gj = geojsonGrupe();
        if (a.vrsta === 'vodotok' && a.vrstaObjekta === 'povrsina-vode') {
          if (!gj.features.length) throw new Error('Površina ne može ostati prazna. Nacrtajte je ili pritisnite Odustani.');
          return ['/api/watercourses/geometry', { code: a.kod, geojson: JSON.stringify(gj), samo_crte: false }];
        }
        if (a.vrsta === 'vodotok') {
          var crte = gj.features.filter(function (f) { return /LineString$/.test(f.geometry.type); });
          if (!crte.length) throw new Error('Tok ne može ostati prazan. Nacrtajte crtu toka ili pritisnite Odustani.');
          var cijeli = novaStacionaza && !a.stacionazaEnc;
          return ['/api/watercourses/geometry', { code: a.kod, geojson: JSON.stringify(cijeli ? gj : { type: 'FeatureCollection', features: crte }), samo_crte: !cijeli }];
        }
        if (!gj.features.length && !window.confirm('Na karti nije ostalo ništa. Obrisati crtu objekta? Prethodna ostaje u povijesti zapisa.')) return null;
        return ['/api/structures/geometry', { id: a.id, geojson: gj.features.length ? JSON.stringify(gj) : '' }];
      }

      function spremi() {
        var gumb = el('.karta-ucrtavanje-spremi'), a = aktivno, z;
        greska('');
        try { z = zahtjev(a); } catch (e) { greska(e.message); return; }
        if (!z) return;
        gumb.disabled = true;
        posalji(z[0], z[1]).then(function () {
          gumb.disabled = false;
          skriveni = [];
          zavrsi();
          k.spremljeno(a);
          if (window.showToast) window.showToast('Spremljeno: ' + a.naziv + '.');
        }).catch(function (e) {
          gumb.disabled = false;
          greska('Nije spremljeno: ' + e.message);
        });
      }

      el('.karta-ucrtavanje-odustani').addEventListener('click', odustani);
      el('.karta-ucrtavanje-natrag').addEventListener('click', odustani);
      el('.karta-ucrtavanje-spremi').addEventListener('click', spremi);
      el('.karta-ucrtavanje-prekini').addEventListener('click', function () { prekidaj(!prekid); });
      el('.karta-ucrtavanje-preklapanje-pokazi').addEventListener('click', pokaziPreklapanje);
      el('.karta-stacionaza-generiraj').addEventListener('click', generiraj);
      el('.karta-ucrtavanje-stacionaza').addEventListener('toggle', osvjeziMjeru);
      // oznake km se samo skrivaju; točke ostaju u crtežu i spremaju se
      el('.karta-stacionaza-po-dionicama').addEventListener('change', osvjeziMjeru);
      var oznake = el('.karta-ucrtavanje-oznake');
      oznake.addEventListener('change', function () { okvir.classList.toggle('karta-bez-oznaka-km', !oznake.checked); });
      var datoteka = el('.karta-ucrtavanje-datoteka');
      datoteka.accept = window.datotekaKarte.prihvaca;
      datoteka.addEventListener('change', function (e) {
        if (e.target.files[0]) ucitaj(e.target.files[0]);
        e.target.value = '';
      });
      karta.on('pm:create', function (e) {
        if (!aktivno || !grupa) return;
        var l = e.layer;
        l.options.pmIgnore = false;
        L.PM.reInitLayer(l);
        if (l instanceof L.Polyline && !(l instanceof L.Polygon) && nastavi(l)) { osvjeziMjeru(); return; }
        if (l instanceof L.Marker && S_KM[aktivno.vrstaObjekta]) {
          var km = (window.prompt('Km stacionaže za ovu točku (npr. 1+250). Ostavite prazno za običnu točku.', '') || '').trim();
          if (km) {
            grupa.removeLayer(l);
            grupa.addLayer(tockaKm(l.getLatLng(), { tip: 'km', km: km }));
          }
        }
        osvjeziMjeru();
      });

      function ucitajJson(adresa, poruka) {
        return fetch(adresa, { credentials: 'same-origin' }).then(function (r) {
          if (r.ok) return r.json();
          return r.text().then(function (t) { throw new Error(t.trim() || poruka); });
        }, function () { throw new Error(poruka); }).catch(function (e) {
          if (window.showToast) window.showToast(e.message || poruka);
          throw e;
        });
      }

      // uredi: p.vrsta 'letva' s p.id i p.ll, p.kod za vodotok, inače p.id objekta
      function uredi(p) {
        if (aktivno) return Promise.resolve();
        if (p.vrsta === 'letva') {
          pocni({ vrsta: 'letva', id: p.id, naziv: p.naziv, adresa: p.adresa }, { ll: p.ll });
          return Promise.resolve();
        }
        if (p.kod) {
          return ucitajJson('/karta/vodotok/' + encodeURIComponent(p.kod), 'Tok vode se nije učitao.').then(function (d) {
            pocni({ vrsta: 'vodotok', kod: d.kod, naziv: d.naziv, adresa: d.adresa, vrstaObjekta: d.povrsina ? 'povrsina-vode' : 'vodotok',
              stacionazaEnc: !!d.stacionaza_enc, granice: p.granice },
              d.geojson ? JSON.parse(d.geojson) : null);
          });
        }
        return ucitajJson('/karta/objekt/' + encodeURIComponent(p.id), 'Objekt se nije učitao.').then(function (d) {
          var gj = d.geojson ? JSON.parse(d.geojson) : { type: 'FeatureCollection', features: [] };
          if (!gj.features.length && d.lat != null && d.lon != null && !CRTE[d.vrsta]) {
            gj.features.push({ type: 'Feature', properties: { tip: 'tocka' }, geometry: { type: 'Point', coordinates: [d.lon, d.lat] } });
          }
          pocni({ vrsta: 'objekt', id: d.id, naziv: d.naziv, adresa: d.adresa, vrstaObjekta: d.vrsta, duljina_km: d.duljina_km,
            odKm: d.od_km, gdje: d.gdje, odsjeci: d.odsjeci || [], granice: p.granice }, gj);
        });
      }

      return { uredi: uredi, radi: function () { return !!aktivno; } };
    }
  };
})();
