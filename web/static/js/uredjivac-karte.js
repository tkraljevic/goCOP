// Zajednički dio uređivača na karti (Leaflet-Geoman): hrvatski natpisi,
// gumbi Uredi, Spremi i Odustani i slanje na poslužitelj. Koriste ga karta
// objekta (uredjivac-objekta.js) i karta vodotoka (uredjivac-vodotoka.js);
// svaka kaže koje alate nudi, kako označi novi dio i što šalje.
(function () {
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
    actions: {
      finish: 'Završi',
      cancel: 'Odustani',
      removeLastVertex: 'Makni zadnju točku'
    },
    buttonTitles: {
      drawMarkerButton: 'Nova točka',
      drawPolyButton: 'Novi poligon',
      drawLineButton: 'Nova crta',
      drawCircleButton: 'Krug',
      drawRectButton: 'Pravokutnik',
      editButton: 'Pomiči točke i vrhove (desni klik briše vrh)',
      dragButton: 'Pomakni cijeli dio objekta',
      cutButton: 'Izreži dio poligona',
      deleteButton: 'Obriši dio objekta',
      drawCircleMarkerButton: 'Kružna oznaka',
      snappingButton: 'Prianjanje',
      pinningButton: 'Spoji vrhove',
      rotateButton: 'Okreni',
      drawTextButton: 'Tekst'
    }
  };

  // natpisi karte: zajednički, a karta smije promijeniti pojedine naslove
  // gumba (vodotok kaže „dio toka”, ne „dio objekta”)
  function natpisi(naslovi) {
    if (!naslovi) return HR;
    var gumbi = {};
    Object.keys(HR.buttonTitles).forEach(function (k) { gumbi[k] = HR.buttonTitles[k]; });
    Object.keys(naslovi).forEach(function (k) { gumbi[k] = naslovi[k]; });
    return { tooltips: HR.tooltips, actions: HR.actions, buttonTitles: gumbi };
  }

  // crta s manje od dvije točke nije crta; ostale značajke ostaju kakve jesu
  function ocistiCrtu(f) {
    var g = f.geometry;
    if (g.type === 'LineString') return g.coordinates.length >= 2 ? f : null;
    if (g.type !== 'MultiLineString') return f;
    g.coordinates = g.coordinates.filter(function (dio) { return dio.length >= 2; });
    return g.coordinates.length > 0 ? f : null;
  }

  // naKarti vraća GeoJSON onoga što se na karti vidi. Kad se crti obrišu
  // vrhovi do zadnje točke, Geoman je makne s karte, a u sloju ostane prazna;
  // takva se ne šalje, inače je program odbije, a korisnik je ne vidi.
  function naKarti(karta, sloj) {
    var znacajke = [];
    sloj.eachLayer(function (l) {
      if (!karta.hasLayer(l) || !l.toGeoJSON) return;
      var gj = l.toGeoJSON();
      (gj.type === 'FeatureCollection' ? gj.features : [gj]).forEach(function (f) {
        var cista = f.geometry ? ocistiCrtu(f) : null;
        if (cista) znacajke.push(cista);
      });
    });
    return { type: 'FeatureCollection', features: znacajke };
  }

  // uredjivacKarte pali uređivač na karti koju je app.js nacrtao u okviru.
  // o.sloj je sloj koji se uređuje, o.kontrole alati na traci, o.adresa kamo
  // se sprema, o.stil izgled nove crte, o.nova(sloj) označi novo nacrtani
  // dio, a o.tijelo(geojson) vrati što se šalje (ili null kad se ne šalje ništa).
  window.uredjivacKarte = function (okvir, o) {
    var sekcija = okvir.closest('.detail-section');
    var gumbUredi = sekcija && sekcija.querySelector('.karta-uredi-btn');
    var gumbSpremi = sekcija && sekcija.querySelector('.karta-spremi-btn');
    var gumbOdustani = sekcija && sekcija.querySelector('.karta-odustani-btn');
    var karta = okvir._karta, sloj = o.sloj;
    if (!gumbUredi || !karta || !sloj || !karta.pm) return;

    karta.pm.setLang('hr', natpisi(o.naslovi), 'en');
    var opcije = { layerGroup: sloj, snappable: true, snapDistance: 15 };
    if (o.stil) opcije.pathOptions = o.stil;
    karta.pm.setGlobalOptions(opcije);

    gumbUredi.addEventListener('click', function () {
      gumbUredi.hidden = true;
      gumbSpremi.hidden = false;
      gumbOdustani.hidden = false;
      okvir.classList.add('u-uredjivanju');
      // prozorčić s nazivom smeta pri pomicanju vrhova; vrati se kad se
      // stranica nakon spremanja ili odustajanja ponovno učita
      karta.closePopup();
      sloj.eachLayer(function (l) { if (l.unbindPopup) l.unbindPopup(); });
      karta.pm.addControls(o.kontrole);
    });

    if (o.nova) karta.on('pm:create', function (e) { o.nova(e.layer); });

    gumbOdustani.addEventListener('click', function () { location.reload(); });

    gumbSpremi.addEventListener('click', function () {
      var tijelo = o.tijelo(naKarti(karta, sloj));
      if (!tijelo) return;
      gumbSpremi.disabled = true;
      fetch(o.adresa, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(tijelo)
      }).then(function (res) {
        if (!res.ok) return res.text().then(function (t) { gumbSpremi.disabled = false; alert('Nije spremljeno: ' + t.trim()); });
        location.hash = 'karta';
        location.reload();
      }).catch(function () { gumbSpremi.disabled = false; alert('Nije spremljeno: nema veze s programom.'); });
    });
  };
})();
