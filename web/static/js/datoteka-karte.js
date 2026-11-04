// Čitanje crte iz datoteke: GeoJSON (QGIS, geojson.io, OpenStreetMap) ili
// KML (Google Earth, Moje karte). KML se pretvori u GeoJSON ovdje, a program
// ga zatim provjeri kao i svaki drugi GeoJSON. Koriste ga Karta
// (karta-uredjivanje.js) te stranice objekta i vode (app.js).
(function () {
  var NAJVISE = 20 * 1024 * 1024;

  function koordinate(el) {
    return (el.textContent || '').trim().split(/\s+/).map(function (t) {
      var d = t.split(',');
      return [parseFloat(d[0]), parseFloat(d[1])];
    }).filter(function (p) { return isFinite(p[0]) && isFinite(p[1]); });
  }

  function prvi(el, ime) { return el.getElementsByTagName(ime)[0]; }

  // geometrije jednog Placemarka; MultiGeometry daje više njih
  function geometrije(pm) {
    var out = [];
    Array.prototype.forEach.call(pm.getElementsByTagName('LineString'), function (el) {
      var c = prvi(el, 'coordinates'), t = c ? koordinate(c) : [];
      if (t.length >= 2) out.push({ type: 'LineString', coordinates: t });
    });
    Array.prototype.forEach.call(pm.getElementsByTagName('Polygon'), function (el) {
      var vanjski = prvi(el, 'outerBoundaryIs') || el, c = prvi(vanjski, 'coordinates'), t = c ? koordinate(c) : [];
      if (t.length >= 4) out.push({ type: 'Polygon', coordinates: [t] });
    });
    Array.prototype.forEach.call(pm.getElementsByTagName('Point'), function (el) {
      var c = prvi(el, 'coordinates'), t = c ? koordinate(c) : [];
      if (t.length) out.push({ type: 'Point', coordinates: t[0] });
    });
    return out;
  }

  // kmlUGeojson: svaki Placemark sa svojim imenom; bez stila i opisa
  function kmlUGeojson(tekst) {
    var doc = new DOMParser().parseFromString(tekst, 'application/xml');
    if (doc.getElementsByTagName('parsererror').length) throw new Error('KML nije ispravan XML');
    var znacajke = [];
    Array.prototype.forEach.call(doc.getElementsByTagName('Placemark'), function (pm) {
      var ime = prvi(pm, 'name'), svojstva = {};
      if (ime && ime.textContent.trim()) svojstva.naziv = ime.textContent.trim();
      geometrije(pm).forEach(function (g) {
        znacajke.push({ type: 'Feature', properties: Object.assign({}, svojstva), geometry: g });
      });
    });
    return { type: 'FeatureCollection', features: znacajke };
  }

  function jeKml(datoteka, tekst) {
    return /\.kml$/i.test(datoteka.name) || /^\s*(<\?xml[^>]*>\s*)?<kml[\s>]/i.test(tekst);
  }

  // procitaj vraća obećanje s GeoJSON zbirkom; greška kaže što ne valja
  function procitaj(datoteka) {
    if (datoteka.size > NAJVISE) return Promise.reject(new Error('Datoteka je veća od 20 MB. Pojednostavnite crtu prije učitavanja.'));
    return datoteka.text().then(function (tekst) {
      var gj;
      try {
        gj = jeKml(datoteka, tekst) ? kmlUGeojson(tekst) : JSON.parse(tekst);
      } catch (e) {
        throw new Error('Datoteka „' + datoteka.name + '” nije ispravan GeoJSON ni KML.');
      }
      if (gj && gj.type === 'Feature') gj = { type: 'FeatureCollection', features: [gj] };
      if (!gj || !Array.isArray(gj.features) || !gj.features.length) throw new Error('U datoteci „' + datoteka.name + '” nema crte ni točke.');
      return gj;
    });
  }

  window.datotekaKarte = { procitaj: procitaj, kmlUGeojson: kmlUGeojson, prihvaca: '.geojson,.json,.kml,application/geo+json,application/json,application/vnd.google-earth.kml+xml' };
})();
