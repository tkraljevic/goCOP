// Čitanje crte iz datoteke: GeoJSON (QGIS, geojson.io, OpenStreetMap) ili
// KML (Google Earth, Moje karte). KML se pretvori u GeoJSON ovdje, a program
// ga zatim provjeri kao i svaki drugi GeoJSON. Koriste ga Karta
// (karta-uredjivanje.js) te stranice objekta i vode (app.js). Izvoz radi
// obrnuto: crtež s Karte u GeoJSON ili KML za Google Earth.
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

  function xml(t) {
    return String(t == null ? '' : t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function kmlKoordinate(niz) {
    return niz.map(function (c) { return c[0] + ',' + c[1]; }).join(' ');
  }

  // KML geometrija jedne GeoJSON geometrije; više dijelova ide u MultiGeometry
  function kmlGeometrija(g) {
    switch (g.type) {
    case 'Point': return '<Point><coordinates>' + kmlKoordinate([g.coordinates]) + '</coordinates></Point>';
    case 'MultiPoint': return '<MultiGeometry>' + g.coordinates.map(function (c) { return kmlGeometrija({ type: 'Point', coordinates: c }); }).join('') + '</MultiGeometry>';
    case 'LineString': return '<LineString><coordinates>' + kmlKoordinate(g.coordinates) + '</coordinates></LineString>';
    case 'MultiLineString': return '<MultiGeometry>' + g.coordinates.map(function (c) { return kmlGeometrija({ type: 'LineString', coordinates: c }); }).join('') + '</MultiGeometry>';
    case 'Polygon': return '<Polygon><outerBoundaryIs><LinearRing><coordinates>' + kmlKoordinate(g.coordinates[0]) + '</coordinates></LinearRing></outerBoundaryIs></Polygon>';
    case 'MultiPolygon': return '<MultiGeometry>' + g.coordinates.map(function (c) { return kmlGeometrija({ type: 'Polygon', coordinates: c }); }).join('') + '</MultiGeometry>';
    }
    return '';
  }

  // geojsonUKml: svaka značajka je Placemark; točka km nosi km kao ime
  function geojsonUKml(gj, naziv) {
    var mjesta = (gj.features || []).map(function (f) {
      var p = f.properties || {}, ime = p.km || (p.rkm != null ? p.oznaka || String(p.rkm) : '') || p.naziv || naziv;
      var g = f.geometry && kmlGeometrija(f.geometry);
      return g ? '<Placemark><name>' + xml(ime) + '</name>' + g + '</Placemark>' : '';
    }).join('\n');
    return '<?xml version="1.0" encoding="UTF-8"?>\n<kml xmlns="http://www.opengis.net/kml/2.2"><Document><name>' + xml(naziv) + '</name>\n' + mjesta + '\n</Document></kml>\n';
  }

  // ime datoteke od naziva: mala slova, bez dijakritika i razmaka
  function imeDatoteke(naziv, nastavak) {
    var t = String(naziv || 'crtez').toLowerCase().replace(/đ/g, 'd').normalize('NFD').replace(/[\u0300-\u036f]/g, '')
      .replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    return (t || 'crtez') + '.' + nastavak;
  }

  // preuzmi spremi crtež u datoteku na računalu (GeoJSON ili KML)
  function preuzmi(gj, naziv, vrsta) {
    var kml = vrsta === 'kml';
    var tekst = kml ? geojsonUKml(gj, naziv) : JSON.stringify(gj);
    var blob = new Blob([tekst], { type: kml ? 'application/vnd.google-earth.kml+xml' : 'application/geo+json' });
    var a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = imeDatoteke(naziv, kml ? 'kml' : 'geojson');
    document.body.appendChild(a);
    a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
  }

  window.datotekaKarte = { procitaj: procitaj, kmlUGeojson: kmlUGeojson, geojsonUKml: geojsonUKml, preuzmi: preuzmi,
    prihvaca: '.geojson,.json,.kml,application/geo+json,application/json,application/vnd.google-earth.kml+xml' };
})();
