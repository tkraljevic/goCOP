// Obrazac inventure (crtež 19b, 8a): razlika prema knjizi računa se dok se
// upisuje prebrojano, a ispod tablice piše koliko je redaka s razlikom.
// Broj se čita kao na poslužitelju: razmaci se preskaču, zarez je decimalni.
(function () {
  var tablica = document.querySelector('.sr-inv-obrazac[data-razlike]');
  if (!tablica) return;
  var zbroj = document.querySelector('[data-zbroj-razlika]');
  function broj(v) {
    var n = parseFloat(String(v || '').replace(/\s/g, '').replace(',', '.'));
    return isNaN(n) ? 0 : n;
  }
  function hr(n) {
    var a = Math.abs(n), dijelovi = String(Math.round(a * 1000) / 1000).split('.');
    var cijeli = dijelovi[0].replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
    return (n > 0 ? '+' : '−') + cijeli + (dijelovi[1] ? ',' + dijelovi[1] : '');
  }
  function racunaj() {
    var manjak = 0, visak = 0;
    tablica.querySelectorAll('tr[data-red]').forEach(function (tr) {
      var r = Math.round((broj(tr.querySelector('input[name^="u:"]').value) - (parseFloat(tr.querySelector('input[name^="k:"]').value) || 0)) * 1000) / 1000;
      var c = tr.querySelector('.sr-inv-razlika');
      c.textContent = r ? hr(r) : '';
      c.classList.toggle('mts-minus', r < 0);
      c.classList.toggle('mts-plus', r > 0);
      if (r < 0) { manjak++; } else if (r > 0) { visak++; }
    });
    var n = manjak + visak;
    if (zbroj) {
      zbroj.textContent = n ? 'S razlikom: ' + n + ' ' + uzBroj(n, 'redak', 'retka', 'redaka') + ' (manjak ' + manjak + ', višak ' + visak + ').' : 'Bez razlike prema knjizi.';
    }
  }
  tablica.addEventListener('input', racunaj);
  racunaj();
})();
