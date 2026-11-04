// Računica mjere na karti, zajednička ravnalu (ravnalo-karte.js), mjeri
// dok se crta (mjera-karte.js) i uređivaču. Udaljenost i površina računaju
// se na kugli; duljina do 1 km piše se u metrima, iznad toga u kilometrima,
// površina do 1 ha u m², iznad toga u hektarima.
(function () {
  var R = 6371000; // polumjer Zemlje u metrima, isti kao u Leafletovu distance

  function broj(n, dec) {
    return Number(n).toLocaleString('hr-HR', { minimumFractionDigits: dec, maximumFractionDigits: dec });
  }

  // udaljenost dviju točaka po kugli ({lat, lng}, u metrima)
  function udaljenost(a, b) {
    var r = Math.PI / 180, f1 = a.lat * r, f2 = b.lat * r;
    var h = Math.pow(Math.sin((f2 - f1) / 2), 2) + Math.cos(f1) * Math.cos(f2) * Math.pow(Math.sin((b.lng - a.lng) * r / 2), 2);
    return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
  }

  function duljina(tocke) {
    var d = 0;
    for (var i = 1; i < tocke.length; i++) d += udaljenost(tocke[i - 1], tocke[i]);
    return d;
  }

  function opseg(tocke) {
    return tocke.length < 2 ? 0 : duljina(tocke) + udaljenost(tocke[tocke.length - 1], tocke[0]);
  }

  // površina poligona na kugli, u m² (isti račun kao geodesicArea u Leaflet.draw)
  function povrsina(tocke) {
    if (tocke.length < 3) return 0;
    var r = Math.PI / 180, a = 0;
    for (var i = 0; i < tocke.length; i++) {
      var p1 = tocke[i], p2 = tocke[(i + 1) % tocke.length];
      a += (p2.lng - p1.lng) * r * (2 + Math.sin(p1.lat * r) + Math.sin(p2.lat * r));
    }
    return Math.abs(a * R * R / 2);
  }

  function metri(m) {
    return m < 1000 ? broj(Math.round(m), 0) + ' m' : broj(m / 1000, 2) + ' km';
  }

  function hektari(m2) {
    return m2 < 10000 ? broj(Math.round(m2), 0) + ' m²' : broj(m2 / 10000, 2) + ' ha';
  }

  // km stacionaže kao 12+440
  function km3(km) {
    var w = Math.floor(km + 1e-9), m = Math.round((km - w) * 1000);
    if (m === 1000) { w++; m = 0; }
    return w + '+' + ('00' + m).slice(-3);
  }

  // čita 12+440, 12+44 ili 12,44
  function citajKm(t) {
    var m = /^\s*(\d+)\s*\+\s*(\d{1,3})\s*$/.exec(t || '');
    if (m) return parseInt(m[1], 10) + parseInt(m[2], 10) / Math.pow(10, m[2].length);
    var x = parseFloat(String(t || '').replace(',', '.'));
    return isFinite(x) ? x : null;
  }

  // mjerilo kao 1 : 48 300 (tvrdi razmaci, da se ne lomi u retku)
  function mjerilo(n) {
    return '1 : ' + broj(Math.round(n), 0).replace(/\./g, ' ');
  }

  // lokalna ravnina u metrima oko lat0, za udaljenost točke od crte
  function xy(ll, lat0) {
    var r = Math.PI / 180;
    return { x: ll.lng * r * R * Math.cos(lat0 * r), y: ll.lat * r * R };
  }

  // najbliže mjesto na crti (niz {lat, lng}): udaljenost od crte, metri od
  // početka crte do tog mjesta, odsječak i sama točka
  function naCrti(crta, ll) {
    if (!crta.length) return null;
    var lat0 = ll.lat, q = xy(ll, lat0), pre = 0, naj = null;
    for (var i = 0; i + 1 < crta.length; i++) {
      var a = xy(crta[i], lat0), b = xy(crta[i + 1], lat0);
      var dx = b.x - a.x, dy = b.y - a.y, d2 = dx * dx + dy * dy, duz = Math.sqrt(d2);
      var u = d2 ? Math.max(0, Math.min(1, ((q.x - a.x) * dx + (q.y - a.y) * dy) / d2)) : 0;
      var px = a.x + u * dx, py = a.y + u * dy, r = Math.sqrt((q.x - px) * (q.x - px) + (q.y - py) * (q.y - py));
      if (!naj || r < naj.udaljenost) {
        naj = { udaljenost: r, s: pre + u * duz, seg: i, ll: { lat: crta[i].lat + u * (crta[i + 1].lat - crta[i].lat), lng: crta[i].lng + u * (crta[i + 1].lng - crta[i].lng) } };
      }
      pre += duz;
    }
    if (!naj) naj = { udaljenost: Math.sqrt(Math.pow(q.x - xy(crta[0], lat0).x, 2) + Math.pow(q.y - xy(crta[0], lat0).y, 2)), s: 0, seg: 0, ll: crta[0] };
    return naj;
  }

  // dio crte od s1 do s2 metara od početka (s1 < s2)
  function dioCrte(crta, s1, s2) {
    var out = [], pre = 0;
    for (var i = 0; i + 1 < crta.length; i++) {
      var a = crta[i], b = crta[i + 1], duz = udaljenost(a, b), kraj = pre + duz;
      var t = function (s) { var u = duz ? (s - pre) / duz : 0; return { lat: a.lat + u * (b.lat - a.lat), lng: a.lng + u * (b.lng - a.lng) }; };
      if (kraj >= s1 && pre <= s2) {
        if (!out.length) out.push(t(Math.max(s1, pre)));
        if (kraj <= s2) out.push(b); else { out.push(t(s2)); break; }
      }
      pre = kraj;
    }
    return out;
  }

  var mjera = {
    udaljenost: udaljenost, duljina: duljina, opseg: opseg, povrsina: povrsina, metri: metri, hektari: hektari,
    km3: km3, citajKm: citajKm, mjerilo: mjerilo, naCrti: naCrti, dioCrte: dioCrte, broj: broj
  };
  if (typeof window !== 'undefined') window.mjera = mjera;
  if (typeof module !== 'undefined') module.exports = mjera;
})();
