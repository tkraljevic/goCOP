// Ispis Karte na papir ili u PDF: format A4 do A0, uspravno ili položeno, i
// mjerilo za odabrani okvir. Okvir se povlači po karti bilo gdje unutra, a
// kut mijenja područje. Mjerilo je obavezno: „Kao na zaslonu” ispisuje okvir kakav jest,
// uz izračunato mjerilo, a okruglo mjerilo namjesti okvir točno na sebe.
// Ispis ide kroz preglednik: karta se za to vrijeme postavi na veličinu
// papira i točno mjerilo, pa se vrati kakva je bila.
(function () {
  var PAPIRI = { A4: [210, 297], A3: [297, 420], A2: [420, 594], A1: [594, 841], A0: [841, 1189] };
  var RUB = 10; // mm, rub stranice na pisaču
  var MJERILA = [5000, 10000, 25000, 50000, 100000];
  var OPSEG = 2 * Math.PI * 6371000; // m, isti polumjer kao mjera i Leafletova traka mjerila
  var PX_MM = 96 / 25.4; // točaka CSS-a u milimetru papira
  var PLAVA = '#173e74';
  var SVIJET = [[-85, -180], [85, -180], [85, 180], [-85, 180]];

  function M() { return window.mjera; }
  function zapamti(k, v) { try { localStorage.setItem('gocop.ispis.' + k, v); } catch (e) { /* bez pamćenja */ } }
  function sjeti(k, zadano) { try { return localStorage.getItem('gocop.ispis.' + k) || zadano; } catch (e) { return zadano; } }

  function pokreni(o) {
    var okvir = o.okvir, karta = o.karta, pogled = okvir.querySelector('.karta-pogled-ispis');
    if (!pogled) return null;
    var el = function (s) { return pogled.querySelector(s); };
    var stanje = { format: sjeti('format', 'A3'), polozaj: sjeti('polozaj', 'polozeno'), mjerilo: null, sredina: null, sirina: 0 };
    if (!PAPIRI[stanje.format]) stanje.format = 'A3';
    var sloj = null, dio = null, prethodni = null, ispisuje = false;

    function papir() {
      var p = PAPIRI[stanje.format], kraca = Math.min(p[0], p[1]), dulja = Math.max(p[0], p[1]);
      return stanje.polozaj === 'polozeno' ? [dulja, kraca] : [kraca, dulja];
    }
    function korisno() { var p = papir(); return [p[0] - 2 * RUB, p[1] - 2 * RUB]; }
    function omjer() { var k = korisno(); return k[0] / k[1]; }
    // mjerilo okvira na papiru, zaokruženo na stotice; isto ide na ispis
    function mjeriloOkvira() { return Math.max(100, Math.round(stanje.sirina / (korisno()[0] / 1000) / 100) * 100); }
    function mjeriloIspisa() { return stanje.mjerilo === 'zaslon' ? mjeriloOkvira() : stanje.mjerilo; }
    function nazivPapira() { return stanje.format + ' ' + (stanje.polozaj === 'polozeno' ? 'položeno' : 'uspravno'); }

    // metri na tlu oko sredine u stupnjeve
    function pomak(c, dx, dy) {
      return L.latLng(c.lat + dy / 111320, c.lng + dx / (111320 * Math.cos(c.lat * Math.PI / 180)));
    }
    function granice() {
      var w = stanje.sirina / 2, h = stanje.sirina / omjer() / 2;
      return L.latLngBounds(pomak(stanje.sredina, -w, -h), pomak(stanje.sredina, w, h));
    }

    // slobodni dio karte: lijevo od ploče na računalu, iznad nje na tabletu
    // i mobitelu (ploča se mjeri; dok se otvara, uzme se njezina uobičajena
    // veličina)
    function slobodno() {
      var vel = karta.getSize(), uski = window.matchMedia('(max-width: 839px)').matches;
      var k = karta.getContainer().getBoundingClientRect(), p = pogled.parentNode.getBoundingClientRect();
      if (p.width && p.height) {
        if (uski) return L.point(vel.x, Math.max(120, Math.min(vel.y, p.top - k.top)));
        return L.point(Math.max(160, Math.min(vel.x, p.left - k.left)), vel.y);
      }
      return L.point(vel.x - (uski ? 0 : 384), uski ? vel.y * 0.45 : vel.y);
    }

    // početni okvir: 80 % slobodnog dijela karte
    function pocetniOkvir() {
      var s = slobodno(), w = s.x * 0.8, h = s.y * 0.8;
      if (w / h > omjer()) w = h * omjer();
      var c = L.point(s.x / 2, s.y / 2);
      stanje.sredina = karta.containerPointToLatLng(c);
      stanje.sirina = M().udaljenost(karta.containerPointToLatLng(c.subtract([w / 2, 0])), karta.containerPointToLatLng(c.add([w / 2, 0])));
    }

    // kad okvir ne stane u slobodni dio karte (npr. A0 u 1 : 5 000), karta se
    // odmakne da se vidi cijeli
    function pokaziOkvir() {
      var g = granice(), s = slobodno(), vel = karta.getSize();
      var a = karta.latLngToContainerPoint(g.getNorthWest()), b = karta.latLngToContainerPoint(g.getSouthEast());
      if (a.x >= 0 && a.y >= 0 && b.x <= s.x && b.y <= s.y) return;
      karta.fitBounds(g, { paddingTopLeft: [24, 24], paddingBottomRight: [vel.x - s.x + 24, vel.y - s.y + 24] });
    }

    // okvir na sredinu onoga što se vidi, iste veličine i mjerila
    function naSredinu() {
      var s = slobodno();
      stanje.sredina = karta.containerPointToLatLng(L.point(s.x / 2, s.y / 2));
      nacrtajOkvir();
      pokaziOkvir();
      osvjeziPlocu();
    }

    function ikona(html, klasa, sidro) {
      return L.divIcon({ className: klasa, html: html, iconSize: null, iconAnchor: sidro });
    }

    function makniOkvir() {
      if (sloj) { karta.removeLayer(sloj); sloj = null; }
      dio = null;
    }

    // vlastito okno za plohu okvira: iznad crta, ispod oznaka na karti
    function okno() {
      if (!karta.getPane('ispisOkvir')) karta.createPane('ispisOkvir').style.zIndex = 590;
      return 'ispisOkvir';
    }

    function presjek(a, b) {
      var j = Math.max(a.getSouth(), b.getSouth()), s = Math.min(a.getNorth(), b.getNorth());
      var z = Math.max(a.getWest(), b.getWest()), i = Math.min(a.getEast(), b.getEast());
      return j < s && z < i ? L.latLngBounds([j, z], [s, i]) : null;
    }

    // povlačenje okvira za natpis ili bilo gdje unutra: okvir ide za mišem
    // ili prstom, a mjerilo ostaje
    function povlaci(m) {
      var pocetak = null;
      m.on('dragstart', function () { pocetak = { ll: m.getLatLng(), sredina: stanje.sredina }; });
      m.on('drag', function () {
        var ll = m.getLatLng();
        stanje.sredina = L.latLng(pocetak.sredina.lat + ll.lat - pocetak.ll.lat, pocetak.sredina.lng + ll.lng - pocetak.ll.lng);
        osvjeziOblik(m);
      });
      m.on('dragend', function () { nacrtajOkvir(); osvjeziPlocu(); });
    }

    // okvir na karti: zatamnjeno izvan, rub, ploha i natpis za povlačenje te
    // četiri kuta
    function nacrtajOkvir() {
      makniOkvir();
      if (!stanje.sredina) return;
      var g = granice(), sz = g.getNorthWest(), si = g.getNorthEast(), jz = g.getSouthWest(), ji = g.getSouthEast();
      sloj = L.layerGroup().addTo(karta);
      dio = { kutovi: [] };
      var nema = { interactive: false, pmIgnore: true, snapIgnore: true };
      // prozirna ploha samo preko vidljivog dijela okvira, da veliki okvir u
      // krupnom zumu ne bude golem element
      var vidi = presjek(g, karta.getBounds().pad(0.5));
      if (vidi) {
        var a = karta.latLngToLayerPoint(vidi.getNorthWest()), b = karta.latLngToLayerPoint(vidi.getSouthEast());
        povlaci(L.marker(vidi.getNorthWest(), { pane: okno(), draggable: true, keyboard: false, pmIgnore: true, snapIgnore: true,
          icon: L.divIcon({ className: 'karta-ispis-ploha', html: '', iconSize: [Math.round(b.x - a.x), Math.round(b.y - a.y)], iconAnchor: [0, 0] }) }).addTo(sloj));
      }
      dio.sjena = L.polygon([SVIJET, [sz, si, ji, jz]], L.extend({ stroke: false, fillColor: '#1a1b1e', fillOpacity: 0.35 }, nema)).addTo(sloj);
      dio.rub = [
        L.rectangle(g, L.extend({ color: '#ffffff', weight: 5, fill: false }, nema)).addTo(sloj),
        L.rectangle(g, L.extend({ color: PLAVA, weight: 2, fill: false }, nema)).addTo(sloj)
      ];
      var tekst = nazivPapira() + (stanje.mjerilo == null ? ' · mjerilo nije odabrano' : ' · ' + M().mjerilo(mjeriloIspisa()));
      dio.natpis = L.marker(sz, { draggable: true, keyboard: false, pmIgnore: true, snapIgnore: true, zIndexOffset: 1000,
        icon: ikona('<span class="karta-ispis-natpis"><strong>' + escapeHtml(tekst) + '</strong> povucite okvir ili njegov kut</span>', 'karta-ispis-natpis-omot', [0, 38]) }).addTo(sloj);
      povlaci(dio.natpis);
      [[sz, ji, -1, 1], [si, jz, 1, 1], [jz, si, -1, -1], [ji, sz, 1, -1]].forEach(function (k) {
        var kut = L.marker(k[0], { draggable: true, keyboard: false, pmIgnore: true, snapIgnore: true, zIndexOffset: 1000,
          icon: ikona('', 'karta-ispis-kut', [7, 7]) }).addTo(sloj);
        dio.kutovi.push(kut);
        var suprotni = k[1];
        kut.on('drag', function () {
          var ll = kut.getLatLng(), w = Math.max(50, M().udaljenost(L.latLng(suprotni.lat, ll.lng), suprotni)), h = w / omjer();
          stanje.sirina = w;
          stanje.sredina = pomak(suprotni, k[2] * w / 2, k[3] * h / 2);
          osvjeziOblik(kut);
        });
        kut.on('dragend', function () {
          // novo područje, novo mjerilo: bira se ponovno
          stanje.mjerilo = null;
          nacrtajOkvir();
          osvjeziPlocu();
        });
      });
    }

    // dok se vuče, zatamnjenje, rub, natpis i kutovi idu za okvirom; ploha
    // se ponovno nacrta kad se pusti
    function osvjeziOblik(vuce) {
      if (!dio) return;
      var g = granice(), uglovi = [g.getNorthWest(), g.getNorthEast(), g.getSouthWest(), g.getSouthEast()];
      dio.sjena.setLatLngs([SVIJET, [uglovi[0], uglovi[1], uglovi[3], uglovi[2]]]);
      dio.rub.forEach(function (r) { r.setBounds(g); });
      if (dio.natpis !== vuce) dio.natpis.setLatLng(uglovi[0]);
      dio.kutovi.forEach(function (k, i) { if (k !== vuce) k.setLatLng(uglovi[i]); });
      osvjeziPlocu();
    }

    // ploha prati vidljivi dio okvira, pa se nacrta iznova kad se karta pomakne
    function naPomakKarte() {
      if (sloj && !ispisuje) nacrtajOkvir();
    }

    function najblize() {
      var s = mjeriloOkvira();
      return MJERILA.reduce(function (a, b) { return Math.abs(Math.log(b / s)) < Math.abs(Math.log(a / s)) ? b : a; });
    }

    function cip(roditelj, tekst, podatak, vrijednost, odabran) {
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'karta-cip';
      b.textContent = tekst;
      b.dataset[podatak] = vrijednost;
      b.setAttribute('aria-pressed', odabran ? 'true' : 'false');
      roditelj.appendChild(b);
      return b;
    }

    function osvjeziPlocu() {
      pogled.querySelectorAll('[data-format]').forEach(function (b) { b.setAttribute('aria-pressed', b.dataset.format === stanje.format ? 'true' : 'false'); });
      pogled.querySelectorAll('[data-polozaj]').forEach(function (b) { b.setAttribute('aria-pressed', b.dataset.polozaj === stanje.polozaj ? 'true' : 'false'); });
      var s = mjeriloOkvira(), naj = najblize(), mjesto = el('.karta-ispis-mjerila');
      el('.karta-ispis-okvir').textContent = 'Odabrani okvir na ' + nazivPapira() + ' je u mjerilu ' + M().mjerilo(s) + '. Ispišite ga tako ili odaberite okruglo mjerilo:';
      mjesto.textContent = '';
      cip(mjesto, 'Kao na zaslonu (' + M().mjerilo(s) + ')', 'mjerilo', 'zaslon', stanje.mjerilo === 'zaslon');
      MJERILA.forEach(function (m) { cip(mjesto, M().mjerilo(m) + (m === naj ? ' · najbliže' : ''), 'mjerilo', String(m), stanje.mjerilo === m); });
      var uputa = el('.karta-ispis-uputa');
      uputa.classList.toggle('karta-ispis-uputa-greska', stanje.mjerilo == null);
      if (stanje.mjerilo == null) uputa.textContent = 'Odaberite mjerilo; bez njega se ne ispisuje, jer na papiru mora pisati mjerilo.';
      else if (stanje.mjerilo === 'zaslon') uputa.textContent = '„Kao na zaslonu” ispisuje okvir kakav jest, uz izračunato mjerilo. Okvir povucite na mjesto koje ispisujete; kut mijenja područje, pa se mjerilo bira ponovno.';
      else uputa.textContent = 'Okvir je namješten na ' + M().mjerilo(stanje.mjerilo) + ': na papiru 1 cm = ' + M().metri(stanje.mjerilo / 100) + '. Okvir povucite na mjesto koje ispisujete; kut mijenja područje, pa se mjerilo bira ponovno.';
      var p = papir();
      el('.karta-ispis-napomena').textContent = nazivPapira() + ' je ' + p[0] + ' × ' + p[1] + ' mm. Na pisaču odaberite isti format i 100 %.' +
        (stanje.format === 'A1' || stanje.format === 'A0' ? ' Za A1 i A0 karta učitava puno pločica, pa priprema traje i do minute.' : '');
      el('.karta-ispis-ispisi').disabled = stanje.mjerilo == null || ispisuje;
    }

    function odaberi(e) {
      var b = e.target.closest('button');
      if (!b) return;
      if (b.dataset.format) {
        stanje.format = b.dataset.format;
        zapamti('format', stanje.format);
      } else if (b.dataset.polozaj) {
        stanje.polozaj = b.dataset.polozaj;
        zapamti('polozaj', stanje.polozaj);
      } else if (b.dataset.mjerilo) {
        stanje.mjerilo = b.dataset.mjerilo === 'zaslon' ? 'zaslon' : +b.dataset.mjerilo;
      } else return;
      // okruglo mjerilo drži okvir na sebi i kad se promijeni papir
      if (typeof stanje.mjerilo === 'number') stanje.sirina = stanje.mjerilo * korisno()[0] / 1000;
      nacrtajOkvir();
      pokaziOkvir();
      osvjeziPlocu();
    }

    function vidljivPogled() {
      return Array.prototype.filter.call(okvir.querySelectorAll('.karta-pogled-slojevi, .karta-pogled-nedostaje'), function (p) { return !p.hidden; })[0];
    }

    function otvori() {
      if (karta._uredjuje) {
        if (window.showToast) window.showToast('Ispis ne radi dok crtate. Spremite ili odustanite od ucrtavanja.');
        return;
      }
      if (!pogled.hidden) { zatvori(); return; }
      if (window.ravnaloKarte) window.ravnaloKarte.iskljuci(karta);
      karta.closePopup();
      prethodni = vidljivPogled();
      if (prethodni) prethodni.hidden = true;
      pogled.hidden = false;
      okvir.classList.add('karta-u-ispisu');
      o.otvoriPlocu();
      stanje.mjerilo = null;
      karta.on('moveend', naPomakKarte);
      // ploča se otvara pa karta treba trenutak da zna svoju veličinu
      setTimeout(function () {
        pocetniOkvir();
        nacrtajOkvir();
        osvjeziPlocu();
      }, 50);
    }

    function zatvori() {
      karta.off('moveend', naPomakKarte);
      makniOkvir();
      pogled.hidden = true;
      okvir.classList.remove('karta-u-ispisu');
      if (prethodni) prethodni.hidden = false;
      prethodni = null;
    }

    // legenda: uključeni slojevi s ploče, sa svojim znakom
    function legenda() {
      var redovi = [];
      okvir.querySelectorAll('.karta-sloj').forEach(function (r) {
        var s = r.querySelector('input[data-sloj]'), naziv = r.querySelector('.karta-sloj-naziv'), znak = r.querySelector('.karta-sloj-znak');
        if (s && s.checked && naziv) redovi.push('<li>' + (znak ? znak.outerHTML : '') + '<span>' + escapeHtml(naziv.textContent.trim()) + '</span></li>');
      });
      return redovi.join('');
    }

    function dodaci(platno, S) {
      var mjesto = document.createElement('div');
      mjesto.className = 'karta-ispis-dodaci';
      var naslov = (el('.karta-ispis-naslov').value || '').trim(), html = '';
      var ukljuceno = function (k) { var c = pogled.querySelector('[data-ispis="' + k + '"]'); return !c || c.checked; };
      if (ukljuceno('legenda')) {
        html += '<div class="karta-ispis-legenda">' + (naslov ? '<strong>' + escapeHtml(naslov) + '</strong>' : '') + '<ul>' + legenda() + '</ul></div>';
      } else if (naslov) html += '<div class="karta-ispis-legenda"><strong>' + escapeHtml(naslov) + '</strong></div>';
      if (ukljuceno('mjerilo')) {
        html += '<div class="karta-ispis-sjever"><svg viewBox="0 0 24 32" aria-hidden="true"><path d="M12 2 20 26l-8-5-8 5Z"/></svg><span>S</span></div>' +
          '<div class="karta-ispis-mjerilo">Mjerilo ' + M().mjerilo(S) + '</div>';
      }
      if (ukljuceno('podnozje')) {
        var sada = new Date(), datum = sada.getDate() + '. ' + (sada.getMonth() + 1) + '. ' + sada.getFullYear() + '. ' +
          ('0' + sada.getHours()).slice(-2) + ':' + ('0' + sada.getMinutes()).slice(-2);
        html += '<div class="karta-ispis-podnozje">' + escapeHtml(datum + ' · podloga: ' + (o.podloga() || 'Karta') +
          (okvir.dataset.korisnik ? ' · ispisao: ' + okvir.dataset.korisnik : '')) + '</div>';
      }
      mjesto.innerHTML = html;
      platno.appendChild(mjesto);
      return mjesto;
    }

    // čeka da se učitaju pločice podloge (najviše 90 s)
    function cekajPlocice() {
      return new Promise(function (gotovo) {
        var pocetak = Date.now();
        (function provjeri() {
          var ucitava = false;
          karta.eachLayer(function (l) { if (l instanceof L.GridLayer && l.isLoading && l.isLoading()) ucitava = true; });
          if ((!ucitava && Date.now() - pocetak > 600) || Date.now() - pocetak > 90000) gotovo();
          else setTimeout(provjeri, 250);
        })();
      });
    }

    function ispisi() {
      if (stanje.mjerilo == null || ispisuje) return;
      ispisuje = true;
      osvjeziPlocu();
      var S = mjeriloIspisa(), k = korisno(), p = papir(), c = stanje.sredina;
      var platno = karta.getContainer(), roditelj = platno.parentNode, sljedeci = platno.nextSibling;
      var staro = { c: karta.getCenter(), z: karta.getZoom(), snap: karta.options.zoomSnap, stil: platno.getAttribute('style') };
      var priprema = document.createElement('div');
      priprema.className = 'karta-ispis-priprema';
      priprema.setAttribute('role', 'status');
      priprema.textContent = 'Pripremam ispis: učitavam kartu za ' + nazivPapira() + ' u mjerilu ' + M().mjerilo(S) + '…';
      var stranica = document.createElement('style');
      stranica.textContent = '@page { size: ' + p[0] + 'mm ' + p[1] + 'mm; margin: ' + RUB + 'mm; }';
      makniOkvir();
      // karta ide sama na stranicu, veličine korisnog dijela papira
      document.head.appendChild(stranica);
      document.body.appendChild(priprema);
      document.body.appendChild(platno);
      document.body.classList.add('karta-ispisuje');
      platno.style.width = Math.round(k[0] * PX_MM) + 'px';
      platno.style.height = Math.round(k[1] * PX_MM) + 'px';
      var dodatak = dodaci(platno, S);
      karta.options.zoomSnap = 0;
      karta.invalidateSize({ animate: false });
      // zum za točno mjerilo: metara po točki CSS-a = S · 2,54 cm / 96
      var zum = Math.log2(OPSEG * Math.cos(c.lat * Math.PI / 180) / (256 * S * 0.0254 / 96));
      karta.setView(c, zum, { animate: false });
      var vraceno = false;
      function naTipku(e) { if (e.key === 'Escape') vrati(); }
      document.addEventListener('keydown', naTipku);
      function vrati() {
        if (vraceno) return;
        vraceno = true;
        document.removeEventListener('keydown', naTipku);
        dodatak.remove();
        priprema.remove();
        stranica.remove();
        document.body.classList.remove('karta-ispisuje');
        roditelj.insertBefore(platno, sljedeci);
        if (staro.stil == null) platno.removeAttribute('style'); else platno.setAttribute('style', staro.stil);
        karta.options.zoomSnap = staro.snap;
        karta.invalidateSize({ animate: false });
        karta.setView(staro.c, staro.z, { animate: false });
        ispisuje = false;
        if (!pogled.hidden) nacrtajOkvir();
        osvjeziPlocu();
      }
      // traka na zaslonu (ne ide na papir): ispis ponovno i povratak na kartu
      function traka() {
        priprema.textContent = '';
        priprema.classList.add('karta-ispis-traka');
        var tekst = document.createElement('span');
        tekst.textContent = 'Karta je postavljena za ' + nazivPapira() + ' u mjerilu ' + M().mjerilo(S) + '.';
        var gumb = function (naziv, klasa, radnja) {
          var b = document.createElement('button');
          b.type = 'button';
          b.className = 'btn btn-sm ' + klasa;
          b.textContent = naziv;
          b.addEventListener('click', radnja);
          priprema.appendChild(b);
          return b;
        };
        priprema.appendChild(tekst);
        gumb('Ispiši ili spremi PDF', 'btn-primary', function () { window.print(); });
        gumb('Natrag na kartu', 'btn-secondary', vrati).focus();
      }
      cekajPlocice().then(function () {
        if (vraceno) return;
        traka();
        var t0 = Date.now();
        window.print();
        // Neki preglednici čekaju dok se ispis ne zatvori, pa se karta odmah
        // vraća. Drugi nastave dok je pregled ispisa još otvoren; tada karta
        // ostaje postavljena za papir dok se ne pritisne „Natrag na kartu” ili
        // Esc, jer bi je povratak promijenio ispod otvorenog ispisa.
        if (Date.now() - t0 > 300) vrati();
      });
    }

    pogled.addEventListener('click', odaberi);
    el('.karta-ispis-zatvori').addEventListener('click', zatvori);
    el('.karta-ispis-odustani').addEventListener('click', zatvori);
    el('.karta-ispis-ispisi').addEventListener('click', ispisi);
    el('.karta-ispis-sredina').addEventListener('click', naSredinu);
    return { otvori: otvori, zatvori: zatvori };
  }

  window.ispisKarte = { pokreni: pokreni };
})();
