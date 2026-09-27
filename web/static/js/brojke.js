// Brojke podataka na naslovnoj i na prijavi: koliko sustav zna. Brojevi se
// odbroje od nule pri dolasku — svaki svojim tempom, veći duže — i kad stanu,
// pločicu prijeđe bljesak. Svake minute stranica ih ponovno pita i odbroji do
// novih: kako stižu očitanja i izdanja prognoza, tako rastu.
(function () {
  var OSVJEZI = 60000, CEKAJ_DESETLJECA = 15000;
  var tiho = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var prije = {}, iscrtano = '';

  function broj(n, dec) {
    return Number(n).toLocaleString('hr-HR', { minimumFractionDigits: dec || 0, maximumFractionDigits: dec || 0 });
  }
  function mjesto(b) { return b >= 1e9 ? broj(b / 1e9, 1) + ' GB' : broj(b / 1e6, 0) + ' MB'; }
  function kratko(n) {
    if (n >= 1e6) { return broj(n / 1e6, n >= 1e7 ? 0 : 1) + ' mil.'; }
    if (n >= 1e3) { return broj(n / 1e3, 0) + ' tis.'; }
    return broj(n);
  }
  function esc(s) { return String(s).replace(/[&<>"]/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]; }); }

  // Odbrojavanje od stare do nove vrijednosti. Pri kraju usporava, a kad
  // stane, pločica bljesne.
  function odbroji(el, od, doVr, dec, trajanje, kasnjenje) {
    var plocica = el.closest('.brojka, .rekord');
    function gotovo() {
      el.textContent = broj(doVr, dec);
      if (plocica && !tiho) {
        plocica.classList.remove('brojka-bljesak');
        void plocica.offsetWidth;
        plocica.classList.add('brojka-bljesak');
      }
    }
    if (tiho || od === doVr) { el.textContent = broj(doVr, dec); return; }
    setTimeout(function () {
      var pocetak = null;
      plocica && plocica.classList.add('brojka-broji');
      function korak(t) {
        if (pocetak === null) { pocetak = t; }
        var u = Math.min(1, (t - pocetak) / trajanje), e = u === 1 ? 1 : 1 - Math.pow(2, -10 * u);
        el.textContent = broj(od + (doVr - od) * e, dec);
        if (u < 1) { requestAnimationFrame(korak); } else { plocica && plocica.classList.remove('brojka-broji'); gotovo(); }
      }
      requestAnimationFrame(korak);
    }, kasnjenje || 0);
  }

  function plocice(b) {
    var velike = [
      { k: 'zapisa', v: b.zapisa, n: 'zapisa u arhivi', o: 'vodostaji, protoci, oborina, snijeg i temperatura' },
      { k: 'gb', v: b.bajtova / 1e9, dec: 1, n: 'GB podataka', o: 'baze i izvorne datoteke nizova' },
      { k: 'godina', v: b.godina, n: 'godina povijesti', o: b.najstarijiDan ? 'najstariji vodostaj: ' + b.najstarijaLetva + ', ' + b.najstarijiDan : '' },
      { k: 'letvi', v: b.letvi, n: 'vodomjernih postaja', o: b.letviUzivo ? broj(b.letviUzivo) + ' javlja uživo u zadnja 24 h' : '' }
    ];
    var male = [
      { k: 'satni', v: b.satnihVodostaja, n: 'satnih vodostaja', o: b.satniOd ? 'od ' + b.satniOd + '.' : '' },
      { k: 'dnevni', v: b.dnevnihVodostaja, n: 'dnevnih vodostaja', o: b.dnevniOd ? 'od ' + b.dnevniOd + '.' : '' },
      { k: 'protok', v: b.protoka, n: 'vrijednosti protoka', o: b.protokOd ? 'od ' + b.protokOd + '.' : '' },
      { k: 'prave', v: b.oborinaPrave, n: 'mjerenja oborine na kišomjerima', o: (b.postajaPravih ? broj(b.postajaPravih) + ' stvarnih postaja' : '') + (b.praveOd ? ', od ' + b.praveOd + '.' : '') },
      { k: 'izvedene', v: b.oborinaIzvedene, n: 'vrijednosti oborine po slivovima', o: (b.tocakaIzvedenih ? broj(b.tocakaIzvedenih) + ' točaka, reanaliza ERA5 i hibridna popuna' : 'reanaliza ERA5 i hibridna popuna') + (b.izvedeneOd ? ', od ' + b.izvedeneOd + '.' : '') },
      { k: 'meteo', v: b.meteo, n: 'vrijednosti snijega i temperature zraka', o: 'po točkama slivova' },
      { k: 'prognoza', v: b.prognoza, n: 'vrijednosti izdanih prognoza', o: b.izdanja ? 'iz ' + broj(b.izdanja) + ' satnih izdanja' : '' },
      { k: 'provjera', v: b.provjera, n: 'prognoza provjerenih unatrag', o: 'satni lanac 2023.–2025. i poplavni valovi' },
      { k: 'ocitanja', v: b.ocitanja, n: 'očitanja u radnoj bazi', o: b.ocitanja24h ? '+' + broj(b.ocitanja24h) + ' u zadnja 24 h' : '' },
      { k: 'profili', v: b.profili, n: 'poprečnih profila korita', o: (b.profilTocke ? broj(b.profilTocke) + ' točaka' : '') + (b.hq ? ', ' + broj(b.hq) + ' krivulja protoka' : '') }
    ];
    var rekordi = (b.rekordi || []).map(function (r, i) { return { k: 'rekord' + i, v: r.cm, r: r }; });
    return {
      velike: velike.filter(function (p) { return p.v > 0; }),
      male: male.filter(function (p) { return p.v > 0; }),
      rekordi: rekordi
    };
  }

  // Stupci po desetljećima, u logaritamskom mjerilu: 1900-ih je desetak
  // tisuća zapisa, 2010-ih trideset milijuna — u ravnom mjerilu prva bi
  // desetljeća bila nevidljiva.
  function desetljeca(d) {
    if (!d || !d.length) {
      return '<div class="brojke-desetljeca-cekaj">Brojim zapise po desetljećima kroz cijelu arhivu…</div>';
    }
    // Mjerilo počinje dekadu ispod najmanjeg desetljeća, da se rast vidi:
    // od nule bi i 1900-e stajale na dvije trećine visine.
    var naj = 1, najm = Infinity;
    d.forEach(function (x) {
      naj = Math.max(naj, x.h, x.m);
      [x.h, x.m].forEach(function (n) { if (n > 0) { najm = Math.min(najm, n); } });
    });
    var dno = Math.pow(10, Math.floor(Math.log10(najm === Infinity ? 1 : najm)));
    var log = function (n) {
      return n > 0 ? Math.max(3, 100 * (Math.log10(n) - Math.log10(dno)) / Math.max(0.1, Math.log10(naj) - Math.log10(dno))) : 0;
    };
    return '<div class="brojke-stupci" role="img" aria-label="Zapisi arhive po desetljećima, logaritamsko mjerilo">' + d.map(function (x, i) {
      var naslov = x.d + '-e: ' + broj(x.h) + ' hidroloških i ' + broj(x.m) + ' meteoroloških zapisa';
      return '<div class="brojke-stupac" title="' + naslov + '" style="--i:' + i + '">' +
        '<div class="brojke-stupac-vr">' + kratko(x.h + x.m) + '</div>' +
        '<div class="brojke-stupac-par">' +
        '<span class="brojke-trak hidro" style="--h:' + log(x.h).toFixed(1) + '%"></span>' +
        '<span class="brojke-trak meteo" style="--h:' + log(x.m).toFixed(1) + '%"></span></div>' +
        '<div class="brojke-stupac-d">' + x.d + '.</div></div>';
    }).join('') + '</div>' +
      '<div class="brojke-stupci-legenda"><span><i class="hidro"></i> hidrologija (vodostaj, protok, temperatura vode)</span>' +
      '<span><i class="meteo"></i> meteorologija (oborina, snijeg, temperatura zraka)</span><span>logaritamsko mjerilo</span></div>';
  }

  function iscrtaj(okvir, b) {
    var p = plocice(b), i = 0;
    var html = '<div class="brojke-glava"><h2 class="brojke-naslov">Podaci u sustavu</h2>' +
      '<span class="brojke-uzivo"><span class="pulse-dot"></span> raste uživo · <span class="brojke-kad"></span></span></div>' +
      '<div class="brojke-velike">' + p.velike.map(function (x) {
        return '<div class="brojka brojka-velika" style="--i:' + (i++) + '"><div class="brojka-vr" data-k="' + x.k + '" data-dec="' + (x.dec || 0) + '">0</div>' +
          '<div class="brojka-naziv">' + x.n + '</div>' + (x.o ? '<div class="brojka-opis" data-o="' + x.k + '">' + esc(x.o) + '</div>' : '') + '</div>';
      }).join('') + '</div>' +
      '<div class="brojke-male">' + p.male.map(function (x) {
        return '<div class="brojka" style="--i:' + (i++) + '"><div class="brojka-vr" data-k="' + x.k + '" data-dec="0">0</div>' +
          '<div class="brojka-naziv">' + x.n + '</div>' + (x.o ? '<div class="brojka-opis" data-o="' + x.k + '">' + esc(x.o) + '</div>' : '') + '</div>';
      }).join('') + '</div>';
    html += '<div class="brojke-dva">';
    html += '<div class="brojke-blok"><h3 class="brojke-podnaslov">Arhiva po desetljećima</h3><div class="brojke-desetljeca">' + desetljeca(b.desetljeca) + '</div></div>';
    if (p.rekordi.length) {
      var vode = [];
      p.rekordi.forEach(function (x) { if (vode.indexOf(x.r.voda) < 0) { vode.push(x.r.voda); } });
      html += '<div class="brojke-blok"><h3 class="brojke-podnaslov">Najviši izmjereni vodostaji</h3>' + vode.map(function (v) {
        return '<div class="rekordi-voda"><div class="rekordi-ime">' + esc(v) + '</div><div class="rekordi">' + p.rekordi.filter(function (x) { return x.r.voda === v; }).map(function (x) {
          return '<div class="rekord" style="--i:' + (i++) + '"><div class="rekord-letva">' + esc(x.r.letva) + '</div>' +
            '<div class="rekord-cm"><span class="brojka-vr" data-k="' + x.k + '" data-dec="0">0</span> cm</div>' +
            '<div class="rekord-kad">' + esc(x.r.kad) + '</div></div>';
        }).join('') + '</div></div>';
      }).join('') + '</div>';
    }
    html += '</div>';
    if (b.velicine && b.velicine.length && b.bajtova) {
      html += '<div class="brojke-mjesto" role="img" aria-label="Raspodjela prostora">' + b.velicine.map(function (v, j) {
        return '<span class="brojke-dio d' + (j % 6) + '" style="flex-grow:' + v.bajtova + '" title="' + esc(v.naziv) + ': ' + mjesto(v.bajtova) + '"></span>';
      }).join('') + '</div><ul class="brojke-legenda">' + b.velicine.map(function (v, j) {
        return '<li><span class="brojke-boja d' + (j % 6) + '"></span>' + esc(v.naziv) + ' <strong>' + mjesto(v.bajtova) + '</strong></li>';
      }).join('') + '</ul>';
    }
    okvir.innerHTML = html;
    okvir.hidden = false;
    okvir.classList.toggle('brojke-tiho', tiho);
    prije = {};
    requestAnimationFrame(function () { okvir.classList.add('brojke-ulaz'); });
  }

  function osvjezi(okvir, b) {
    var p = plocice(b), sve = p.velike.concat(p.male, p.rekordi);
    // Kad se promijeni sastav (npr. stignu desetljeća), crtež se složi iznova.
    var sastav = sve.map(function (x) { return x.k; }).join(',') + '|' + ((b.desetljeca || []).length);
    var prvi = sastav !== iscrtano;
    if (prvi) {
      var stare = prije;
      iscrtaj(okvir, b);
      if (iscrtano) { prije = stare; } // već viđeni brojevi ne kreću opet od nule
      iscrtano = sastav;
    }
    sve.forEach(function (x, i) {
      var el = okvir.querySelector('.brojka-vr[data-k="' + x.k + '"]');
      if (!el) { return; }
      var dec = Number(el.dataset.dec || 0), novo = prije[x.k] === undefined;
      var od = novo ? 0 : prije[x.k];
      // Svaki broj svojim tempom: veći broji duže, a pločice kreću jedna za drugom.
      var trajanje = novo ? 1300 + 250 * Math.log10(Math.max(10, x.v)) : 1200;
      odbroji(el, od, x.v, dec, trajanje, novo ? 90 * i : 0);
      if (!novo && x.v > prije[x.k] && !dec) {
        var plocica = el.closest('.brojka, .rekord');
        var rast = document.createElement('span');
        rast.className = 'brojka-rast';
        rast.textContent = '+' + broj(x.v - prije[x.k]);
        (plocica || el.parentNode).appendChild(rast);
        plocica && plocica.classList.add('brojka-raste');
        setTimeout(function () { rast.remove(); plocica && plocica.classList.remove('brojka-raste'); }, 4000);
      }
      var opis = okvir.querySelector('[data-o="' + x.k + '"]');
      if (opis && x.o) { opis.textContent = x.o; }
      prije[x.k] = x.v;
    });
    var kad = okvir.querySelector('.brojke-kad');
    if (kad && b.izracunato) {
      var d = new Date(b.izracunato);
      kad.textContent = 'izbrojeno u ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    }
  }

  function pitaj(okvir) {
    fetch(okvir.dataset.url, { credentials: 'same-origin' })
      .then(function (r) { if (!r.ok) { throw new Error(r.status); } return r.json(); })
      .then(function (b) {
        osvjezi(okvir, b);
        setTimeout(function () { pitaj(okvir); }, b.desetljeca && b.desetljeca.length ? OSVJEZI : CEKAJ_DESETLJECA);
      })
      .catch(function () { if (!iscrtano) { okvir.hidden = true; } setTimeout(function () { pitaj(okvir); }, OSVJEZI); });
  }

  document.addEventListener('DOMContentLoaded', function () {
    var okvir = document.getElementById('podaci-brojke');
    if (okvir && okvir.dataset.url) { pitaj(okvir); }
  });
})();
