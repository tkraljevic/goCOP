// goCOP klijentska skripta — SSE sinkronizacija i responzivna interakcija

document.addEventListener("DOMContentLoaded", () => {
  initSSE();
  initSectorAreaChangers();
  initLiveSearch();
});

// Real-time SSE sinkronizacija
function initSSE() {
  const statusText = document.getElementById("sync-status-text");
  const evtSource = new EventSource("/api/events");

  evtSource.onopen = () => {
    if (statusText) statusText.textContent = "Sinkronizirano (Online)";
  };

  evtSource.addEventListener("users_updated", (e) => {
    const data = JSON.parse(e.data);
    showToast(data.message || "Ažurirani podaci djelatnika");
    setTimeout(() => {
      window.location.reload();
    }, 1200);
  });

  evtSource.addEventListener("user_deleted", (e) => {
    const data = JSON.parse(e.data);
    showToast(data.message || "Profil djelatnika obrisan");
    setTimeout(() => {
      window.location.reload();
    }, 1200);
  });

  evtSource.addEventListener("duty_added", (e) => {
    const data = JSON.parse(e.data);
    showToast(data.message || "Dodijeljena nova funkcija / zaduženje");
    setTimeout(() => {
      window.location.reload();
    }, 1200);
  });

  evtSource.addEventListener("duty_revoked", (e) => {
    const data = JSON.parse(e.data);
    showToast(data.message || "Opozvano zaduženje");
    setTimeout(() => {
      window.location.reload();
    }, 1200);
  });

  evtSource.onerror = () => {
    if (statusText) statusText.textContent = "Povezivanje...";
  };
}

// Prikaz plutajuće toast obavijesti
function showToast(msg) {
  let container = document.getElementById("toast-container");
  if (!container) {
    container = document.createElement("div");
    container.id = "toast-container";
    container.className = "toast-container";
    document.body.appendChild(container);
  }

  const toast = document.createElement("div");
  toast.className = "toast";
  toast.textContent = msg;
  container.appendChild(toast);

  setTimeout(() => {
    toast.remove();
  }, 4000);
}

// Otvaranje i zatvaranje modala
function openModal(id) {
  const modal = document.getElementById(id);
  if (modal) {
    modal.classList.add("active");
  }
}

function closeModal(id) {
  const modal = document.getElementById(id);
  if (modal) {
    modal.classList.remove("active");
  }
}

// Otvaranje modala za uređivanje korisnika
function openEditUserModal(user) {
  const isMe = window.currentUserId === user.id;
  const canAdminUsers = window.isGlobalAdmin || (window.adminSectorsCount > 0) || (window.adminAreasCount > 0);

  document.getElementById("edit-user-id").value = user.id;

  const usernameInput = document.getElementById("edit-user-username");
  if (usernameInput) {
    usernameInput.value = user.username;
    usernameInput.readOnly = isMe && !window.isGlobalAdmin;
  }

  document.getElementById("edit-user-fullname").value = user.full_name;
  document.getElementById("edit-user-title").value = user.title || "";

  const orgTypeSelect = document.getElementById("edit-user-org-type");
  if (orgTypeSelect) {
    orgTypeSelect.value = user.org_type;
    orgTypeSelect.disabled = isMe && !canAdminUsers;
  }

  const orgNameInput = document.getElementById("edit-user-org-name");
  if (orgNameInput) {
    orgNameInput.value = user.org_name || "";
    orgNameInput.readOnly = isMe && !canAdminUsers;
  }

  document.getElementById("edit-user-phone").value = user.phone || "";
  document.getElementById("edit-user-mobile").value = user.mobile_phone || "";
  document.getElementById("edit-user-short").value = user.short_phone || "";
  document.getElementById("edit-user-email").value = user.email || "";

  const adminCheckbox = document.getElementById("edit-user-admin");
  if (adminCheckbox) {
    adminCheckbox.checked = !!user.is_global_admin;
  }

  const activeCheckbox = document.getElementById("edit-user-active");
  const activeLabel = document.getElementById("edit-user-active-label");
  if (activeCheckbox) {
    activeCheckbox.checked = !!user.is_active;
    if (activeLabel) {
      activeLabel.style.display = (isMe && !window.isGlobalAdmin) ? "none" : "flex";
    }
  }

  const deleteBtn = document.getElementById("edit-user-delete-btn");
  if (deleteBtn) {
    deleteBtn.style.display = (isMe || !canAdminUsers) ? "none" : "inline-block";
  }

  const modalTitle = document.querySelector("#modal-edit-user .modal-title");
  if (modalTitle) {
    modalTitle.textContent = isMe ? "👤 Moj profil" : "✏️ Uređivanje profila djelatnika";
  }

  const hintEl = document.getElementById("edit-user-hint");
  if (hintEl) {
    if (isMe) {
      hintEl.textContent = "Ovdje možete urediti svoje kontakt podatke (mobitel, fiksni telefon, lokal, e-mail) i titulu. Službena zaduženja i funkcije dodjeljuje rukovoditelj.";
    } else {
      hintEl.textContent = "";
    }
  }

  openModal("modal-edit-user");
}

// Otvaranje uređenja vlastitog profila (sa početne ili bilo koje stranice)
function openEditMyProfile() {
  const myModal = document.getElementById("modal-my-profile");
  if (myModal) {
    myModal.style.display = "flex";
    return;
  }
  if (typeof openEditUserModal === "function" && window.currentUserObj) {
    openEditUserModal(window.currentUserObj);
  }
}

function closeMyProfileModal() {
  const myModal = document.getElementById("modal-my-profile");
  if (myModal) myModal.style.display = "none";
}

// Otvaranje modala za dodavanje nove funkcije / zaduženja dionica / ispomoći
function openAddDutyModal(userID, userName) {
  document.getElementById("duty-user-id").value = userID;
  document.getElementById("duty-user-name").textContent = userName;
  openModal("modal-add-duty");
}

// Dinamičko punjenje branjenih područja ovisno o odabranom sektoru
function initSectorAreaChangers() {
  const setupChanger = (sectorSelectId, areaSelectId) => {
    const secSelect = document.getElementById(sectorSelectId);
    if (!secSelect) return;

    secSelect.addEventListener("change", () => {
      loadAreasForSelect(sectorSelectId, areaSelectId);
    });
  };

  setupChanger("filter-sector", "filter-area");
  setupChanger("new-duty-sector", "new-duty-area");
  setupChanger("duty-sector", "duty-area");
}

function loadAreasForSelect(sectorSelectId, areaSelectId, preselectedAreaId) {
  const secSelect = document.getElementById(sectorSelectId);
  const areaSelect = document.getElementById(areaSelectId);
  if (!secSelect || !areaSelect) return;

  const sectorID = secSelect.value;
  areaSelect.innerHTML = '<option value="">-- Sva branjena područja --</option>';

  if (!sectorID) return;

  fetch(`/api/areas?sector=${sectorID}`)
    .then((res) => res.json())
    .then((areas) => {
      areas.forEach((a) => {
        const opt = document.createElement("option");
        opt.value = a.id;
        opt.textContent = `${a.id}: ${a.name} (${a.subcenter})`;
        if (preselectedAreaId && preselectedAreaId === a.id) {
          opt.selected = true;
        }
        areaSelect.appendChild(opt);
      });
    })
    .catch((err) => console.error("Greška pri dohvatu područja:", err));
}

// Potvrda brisanja korisničkog profila
function confirmDeleteUser(userID, userName) {
  const idInput = document.getElementById("delete-user-id");
  const nameSpan = document.getElementById("delete-user-name");
  if (idInput && nameSpan) {
    idInput.value = userID;
    nameSpan.textContent = userName;
    openModal("modal-delete-user");
  }
}

function triggerDeleteFromModal() {
  const userID = document.getElementById("edit-user-id").value;
  const userName = document.getElementById("edit-user-fullname").value;
  closeModal("modal-edit-user");
  confirmDeleteUser(userID, userName);
}

// Brza pretraga djelatnika na klijentskoj strani u stvarnom vremenu
function initLiveSearch() {
  const searchInput = document.getElementById("filter-search");
  if (!searchInput) return;

  const rows = document.querySelectorAll(".data-table tbody tr");
  const cards = document.querySelectorAll(".user-card");

  searchInput.addEventListener("input", () => {
    const q = searchInput.value.toLowerCase().trim();
    if (!q) {
      rows.forEach((r) => (r.style.display = ""));
      cards.forEach((c) => (c.style.display = ""));
      return;
    }

    rows.forEach((r) => {
      const text = r.textContent.toLowerCase();
      r.style.display = text.includes(q) ? "" : "none";
    });

    cards.forEach((c) => {
      const text = c.textContent.toLowerCase();
      c.style.display = text.includes(q) ? "" : "none";
    });
  });
}

// Upravljanje modalom za promjenu lozinke
function openChangePasswordModal() {
  const modal = document.getElementById("modal-change-password");
  if (modal) {
    modal.style.display = "flex";
    const curPw = document.getElementById("current_password");
    if (curPw) curPw.focus();
  }
}

function closeChangePasswordModal() {
  const modal = document.getElementById("modal-change-password");
  if (modal) modal.style.display = "none";
}

// Markdown renderer za responzivni prikaz teksta s oblikovanjem (bold, italic, liste, naslovi, odlomci)
function renderMarkdown(md) {
  if (!md) return '';
  let text = String(md).trim();

  // Escape HTML entities radi XSS zaštite
  text = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

  // Naslovi (###, ##, #)
  text = text.replace(/^### (.*$)/gim, '<h5 class="md-h5">$1</h5>');
  text = text.replace(/^## (.*$)/gim, '<h4 class="md-h4">$1</h4>');
  text = text.replace(/^# (.*$)/gim, '<h3 class="md-h3">$1</h3>');

  // Podebljani tekst (**tekst** ili __tekst__)
  text = text.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
  text = text.replace(/__(.*?)__/g, '<strong>$1</strong>');

  // Kurziv (*tekst* ili _tekst_)
  text = text.replace(/\*([^\*]+)\*/g, '<em>$1</em>');
  text = text.replace(/_([^_]+)_/g, '<em>$1</em>');

  // Kôd (`kôd`)
  text = text.replace(/`([^`]+)`/g, '<code class="md-code">$1</code>');

  // Poveznice ([tekst](url))
  text = text.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener" class="md-link">$1</a>');

  // Višelinijsko parsiranje popisa s grafičkim oznakama (- ili *)
  const lines = text.split('\n');
  let inList = false;
  const output = [];

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const trimmed = rawLine.trim();

    // Provjeri je li stavka popisa: počinje s '- ' ili '* '
    const listMatch = rawLine.match(/^(\s*)[-*]\s+(.*)$/);
    if (listMatch) {
      if (!inList) {
        output.push('<ul class="md-list">');
        inList = true;
      }
      output.push(`<li>${listMatch[2]}</li>`);
    } else {
      if (inList) {
        output.push('</ul>');
        inList = false;
      }
      if (trimmed === '') {
        output.push('<div class="md-spacer"></div>');
      } else if (!trimmed.startsWith('<h')) {
        output.push(`<div class="md-p">${trimmed}</div>`);
      } else {
        output.push(trimmed);
      }
    }
  }
  if (inList) {
    output.push('</ul>');
  }

  return output.join('');
}



// Pokazivač na grafu niza: pri prelasku mišem traži najbližu točku po
// vodoravnoj osi i pokazuje njezinu vrijednost i vrijeme. Same točke nisu
// nacrtane — sedamsto kružića bilo bi teška slika i nečitljiva crta — nego
// stoje u data-tocke i skripta crta samo onu nad kojom je miš.
(function () {
  function postavi(box) {
    var svg = box.querySelector('svg');
    var oblacic = box.querySelector('.graf-oblacic');
    var pokazivac = box.querySelector('.pokazivac');
    if (!svg || !oblacic || !pokazivac) return;

    var tocke;
    try { tocke = JSON.parse(box.dataset.tocke || '[]'); } catch (e) { return; }
    if (!tocke.length) return;

    var vodilja = pokazivac.querySelector('.vodilja');
    var biljeg = pokazivac.querySelector('.biljeg');
    var vb = svg.viewBox.baseVal;

    function najbliza(x) {
      var lo = 0, hi = tocke.length - 1;
      while (lo < hi) {
        var sr = (lo + hi) >> 1;
        if (tocke[sr][0] < x) lo = sr + 1; else hi = sr;
      }
      if (lo > 0 && Math.abs(tocke[lo - 1][0] - x) < Math.abs(tocke[lo][0] - x)) lo--;
      return tocke[lo];
    }

    function pomak(ev) {
      var r = svg.getBoundingClientRect();
      if (!r.width) return;
      var x = (ev.clientX - r.left) / r.width * vb.width;
      var t = najbliza(x);
      pokazivac.hidden = false;
      vodilja.setAttribute('x1', t[0]);
      vodilja.setAttribute('x2', t[0]);
      biljeg.setAttribute('cx', t[0]);
      biljeg.setAttribute('cy', t[1]);

      oblacic.hidden = false;
      oblacic.innerHTML = '<strong>' + t[3] + '</strong><span>' + t[2] + '</span>';
      // oblačić prati miša, ali ne izlazi iz okvira
      var lijevo = (t[0] / vb.width) * r.width;
      var sirina = oblacic.offsetWidth || 120;
      lijevo = Math.min(Math.max(lijevo - sirina / 2, 4), r.width - sirina - 4);
      oblacic.style.left = lijevo + 'px';
      oblacic.style.top = ((t[1] / vb.height) * r.height - oblacic.offsetHeight - 10) + 'px';
    }

    function sakrij() {
      pokazivac.hidden = true;
      oblacic.hidden = true;
    }

    svg.addEventListener('mousemove', pomak);
    svg.addEventListener('mouseleave', sakrij);
    // Na dodiru nema odlaska miša: jedan dodir postavi pokazivač i on ostane
    // stajati dok se stranica ne osvježi. Zato se sklanja i na kraj dodira.
    svg.addEventListener('touchend', sakrij);
    svg.addEventListener('touchcancel', sakrij);
    window.addEventListener('scroll', sakrij, { passive: true });
    svg.addEventListener('touchmove', function (ev) {
      if (ev.touches.length) pomak(ev.touches[0]);
    }, { passive: true });
    svg.addEventListener('touchend', sakrij);
  }

  document.addEventListener('DOMContentLoaded', function () {
    document.querySelectorAll('.graf-niza').forEach(postavi);
  });
})();

// Karta položaja letve. Pločice dolaze s mreže, sve ostalo je lokalno — pa
// program bez interneta i dalje radi, samo bez podloge. Kad se pločice jednom
// preuzmu za područje obrane, u postavkama se upiše lokalna putanja i karta
// radi svugdje.
(function () {
  document.addEventListener('DOMContentLoaded', function () {
    if (typeof L === 'undefined') return;
    document.querySelectorAll('.karta-letve').forEach(function (okvir) {
      var platno = okvir.querySelector('.karta-platno');
      var lat = parseFloat(okvir.dataset.lat), lon = parseFloat(okvir.dataset.lon);
      if (!platno || isNaN(lat) || isNaN(lon) || !okvir.dataset.plocice) return;

      var najvise = parseInt(okvir.dataset.najviseZ, 10) || 17;
      var karta = L.map(platno, { scrollWheelZoom: false }).setView([lat, lon], 14);
      var sloj = L.tileLayer(okvir.dataset.plocice, {
        maxZoom: najvise,
        attribution: okvir.dataset.zasluge || ''
      });

      // Ako pločice ne stignu, karta ostaje prazna — bolje je to reći nego
      // pustiti čovjeka da gleda sive kvadrate i misli da je letva nestala.
      var promasaja = 0;
      sloj.on('tileerror', function () {
        if (++promasaja < 3) return;
        var poruka = okvir.querySelector('.karta-bez-mreze');
        if (poruka) poruka.hidden = false;
        okvir.classList.add('karta-prazna');
      });
      sloj.addTo(karta);

      L.marker([lat, lon]).addTo(karta).bindPopup(okvir.dataset.naziv || '');
      // kotačić miša lista stranicu; karta se približava tek na klik
      karta.on('click', function () { karta.scrollWheelZoom.enable(); });
    });
  });
})();

// Tablice na uskom zaslonu. Vodoravno listanje unutar stranice znači da se
// pola tablice nikad ne vidi — prst ne zna koji klizač hvata, a dežurni ne
// zna da desno još nešto piše. Zato se tablica koja ne stane razlaže u
// kartice: svaki redak postaje blok, a naslov stupca ide uz vrijednost.
//
// Koje su to tablice ne pogađa se po broju stupaca nego mjeri: usporedi se
// širina tablice sa širinom okvira. Tablica od tri kratka stupca ostaje
// tablica i na telefonu, jer ondje je ona čitljivija.
(function () {
  var PRAG = 700;          // ispod ove širine prozora se uopće razmatra
  var NAJUZI_STUPAC = 110; // uži stupac lomi svaku vrijednost u nekoliko redaka

  // Naslov stupca uz svaku vrijednost. Uzima se iz zaglavlja, pa se ne mora
  // ponavljati u svakom predlošku — tablica ih ima trideset i četiri.
  function oznaci(tab) {
    if (tab.dataset.stupciOznaceni) return;
    var glave = [].map.call(tab.querySelectorAll('thead th'), function (th) {
      return th.textContent.trim().replace(/\s+/g, ' ');
    });
    if (!glave.length) return;
    [].forEach.call(tab.querySelectorAll('tbody tr'), function (tr) {
      [].forEach.call(tr.children, function (td, i) {
        if (td.hasAttribute('data-stupac') || td.colSpan > 1) return;
        if (glave[i]) td.setAttribute('data-stupac', glave[i]);
      });
    });
    tab.dataset.stupciOznaceni = '1';
  }

  function slozi() {
    [].forEach.call(document.querySelectorAll('.table-responsive'), function (okvir) {
      var tab = okvir.querySelector('table');
      if (!tab) return;
      // Mjeri se bez slaganja, inače tablica uvijek stane.
      okvir.classList.remove('tablica-kartice');
      // Prelijevanje nije jedini način da tablica postane nečitljiva: pet
      // stupaca u 375 točaka "stane" tako da svaku ćeliju slomi u visoki
      // uski stupac. Zato se gleda i koliko prostora ostaje po stupcu.
      var stupaca = tab.querySelectorAll('thead th').length || 1;
      // skrivena tablica ima širinu nula; nju se ne dira
      if (!okvir.clientWidth) return;
      var neStane = tab.scrollWidth > okvir.clientWidth + 1 ||
        okvir.clientWidth / stupaca < NAJUZI_STUPAC;
      if (neStane && window.innerWidth <= PRAG) {
        oznaci(tab);
        okvir.classList.add('tablica-kartice');
      }
    });
  }

  document.addEventListener('DOMContentLoaded', slozi);
  // Listanje mijenja tablice bez novog učitavanja stranice, pa se slaganje
  // mora ponoviti nad onim što je upravo umetnuto.
  document.addEventListener('gocop:sadrzaj', slozi);
  var cekaj;
  window.addEventListener('resize', function () {
    clearTimeout(cekaj);
    cekaj = setTimeout(slozi, 150);
  });
})();

// Listanje bez ponovnog učitavanja stranice. Klik na listač dohvati istu
// stranicu i zamijeni samo popis; ostatak — graf, presjek korita, karta —
// ostaje netaknut, a pogled ne odskače na vrh.
//
// Nadogradnja, ne uvjet: poveznice listača su obične poveznice i bez skripte
// rade kao i dosad. Zato ovdje nema ni HTMX-a ni sličnog: sve što treba stane
// u tridesetak redaka, a program mora raditi i offline i bez skripte.
(function () {
  var uTijeku = null;

  function zamijeni(okvir, url) {
    var kljuc = okvir.getAttribute('data-listanje');
    if (uTijeku) uTijeku.abort();
    uTijeku = typeof AbortController !== 'undefined' ? new AbortController() : null;
    okvir.setAttribute('aria-busy', 'true');

    fetch(url, {
      credentials: 'same-origin',
      headers: { 'X-Listanje': kljuc },
      signal: uTijeku ? uTijeku.signal : undefined
    })
      .then(function (o) {
        if (!o.ok) throw new Error(o.status);
        return o.text();
      })
      .then(function (tekst) {
        var doc = new DOMParser().parseFromString(tekst, 'text/html');
        var novi = doc.querySelector('[data-listanje="' + kljuc + '"]');
        if (!novi) throw new Error('nema popisa u odgovoru');
        okvir.innerHTML = novi.innerHTML;
        okvir.removeAttribute('aria-busy');
        try { history.pushState({ listanje: kljuc }, '', url); } catch (e) { /* file:// */ }
        document.dispatchEvent(new CustomEvent('gocop:sadrzaj', { detail: { okvir: okvir } }));
        // Ako je popis iznad vidljivog dijela — dogodi se kad zadnja stranica
        // ima manje redaka — vrati ga u pogled. Inače se ne dira ništa.
        if (okvir.getBoundingClientRect().top < 0) {
          okvir.scrollIntoView({ block: 'start' });
        }
      })
      .catch(function (e) {
        if (e && e.name === 'AbortError') return;
        window.location.href = url; // pa neka radi kao i bez skripte
      });
  }

  document.addEventListener('click', function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest && e.target.closest('.pager a[href]');
    if (!a) return;
    var okvir = a.closest('[data-listanje]');
    if (!okvir || !window.fetch || !window.DOMParser) return;
    e.preventDefault();
    zamijeni(okvir, a.getAttribute('href'));
  });

  // Natrag i naprijed moraju vratiti stranicu koja je bila. Popis se dohvaća
  // iznova jer se u međuvremenu mogao promijeniti.
  window.addEventListener('popstate', function () { window.location.reload(); });
})();


// Brisanje letve. Jedan „jeste li sigurni" ovdje nije dovoljan: letva nosi
// pragove, kotu nule, ekstreme i veze na dionice, a očitanja upisana na njoj
// NEMAJU vezu s kaskadnim brisanjem — ostaju u bazi bez letve i više se ne mogu
// otvoriti. Zato se posljedice nabroje, pa se traži da se naziv upiše rukom.
(function () {
  document.addEventListener('submit', function (e) {
    var f = e.target;
    if (!f.hasAttribute || !f.hasAttribute('data-brisanje-letve')) return;

    var naziv = f.getAttribute('data-naziv') || '';
    var ocitanja = parseInt(f.getAttribute('data-ocitanja') || '0', 10);
    var dionica = parseInt(f.getAttribute('data-dionice') || '0', 10);

    var posljedice = ['Brisanjem letve „' + naziv + '" nestaju:',
      '  · pragovi obrane i kota nule',
      '  · zabilježene krajnosti i povratni vodostaji'];
    if (dionica > 0) {
      posljedice.push('  · veza s ' + dionica + (dionica === 1 ? ' dionicom' : ' dionice') +
        ' — ondje letva više neće biti mjerodavna');
    }
    if (ocitanja > 0) {
      posljedice.push('');
      posljedice.push(ocitanja + ' očitanja upisanih na ovoj letvi NEĆE se obrisati,');
      posljedice.push('ali ostaju bez letve i više se neće moći otvoriti.');
    }
    posljedice.push('');
    posljedice.push('Zapis ostaje u povijesti verzija i može se vratiti.');

    if (!window.confirm(posljedice.join('\n'))) {
      e.preventDefault();
      return;
    }
    var upisano = window.prompt('Za potvrdu upišite naziv letve točno ovako:\n\n' + naziv);
    if (upisano === null) { e.preventDefault(); return; }
    if (upisano.trim() !== naziv) {
      e.preventDefault();
      window.alert('Naziv se ne poklapa — letva nije obrisana.');
    }
  });
})();

// Odabir retka u nizu iz arhive: kartica iznad pokazuje odabranu vrijednost,
// a ne samo posljednju. Dežurni tako vidi vodostaj, protok iz krivulje i kotu
// vode za bilo koji dan, bez računanja u glavi.
//
// Bez skripte se ne gubi ništa: kartica ostaje na posljednjoj vrijednosti, a
// sve vrijednosti i dalje stoje u tablici.
(function () {
  function polje(kartica, ime) {
    return kartica.querySelector('[data-polje="' + ime + '"]');
  }

  function pisi(kartica, izvor) {
    var v = polje(kartica, 'vodostaj');
    if (v) { v.textContent = izvor.getAttribute('data-vodostaj') + ' cm'; }

    var kad = polje(kartica, 'kad');
    if (kad) { kad.textContent = izvor.getAttribute('data-kad') || ''; }

    var t = polje(kartica, 'tocnost');
    if (t) {
      t.textContent = izvor.getAttribute('data-tocnost') || '';
      t.hidden = !t.textContent;
      t.className = 'badge ' + (izvor.getAttribute('data-tocno') ? 'badge-area-admin' : 'badge-unknown');
    }

    var i = polje(kartica, 'izvor');
    if (i) { i.textContent = izvor.getAttribute('data-izvor') || ''; }

    var q = polje(kartica, 'protok');
    if (q) {
      var protok = izvor.getAttribute('data-protok');
      q.hidden = !protok;
      if (protok) {
        q.innerHTML = 'protok <strong>' + protok + ' m³/s</strong> ' +
          '<span class="reg-card-sub">iz krivulje</span>';
      }
    }

    var k = polje(kartica, 'kote');
    if (k) {
      var kote = (izvor.getAttribute('data-kote') || '').split('|').filter(Boolean);
      k.innerHTML = kote.map(function (red) {
        var dio = red.split(' m ');
        return '<span>kota vode <strong>' + dio[0] + ' m</strong> ' +
          '<span class="reg-card-sub">' + (dio[1] || '') + '</span></span>';
      }).join(' ');
    }
  }

  function odaberi(redak) {
    var kartica = document.getElementById('stanje');
    if (!kartica || !redak) { return; }
    var bio = document.querySelector('.niz-redak.odabran');
    if (bio) { bio.classList.remove('odabran'); }
    if (bio === redak) {
      // ponovni klik vraća posljednju vrijednost
      pisi(kartica, kartica);
      kartica.classList.remove('odabrano');
      return;
    }
    redak.classList.add('odabran');
    pisi(kartica, redak);
    kartica.classList.add('odabrano');
  }

  document.addEventListener('click', function (e) {
    var redak = e.target.closest ? e.target.closest('.niz-redak') : null;
    if (redak) { odaberi(redak); }
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') { return; }
    var redak = e.target.closest ? e.target.closest('.niz-redak') : null;
    if (redak) { e.preventDefault(); odaberi(redak); }
  });
})();

// Traka napretka za dugi posao.
//
// Poslužitelj posao vrti u pozadini i javlja kako stoji na /poslovi/{id}.
// Stranica koja ga je pokrenula ispiše <div class="posao" data-posao="ID">
// i ovo je popuni: traka, čime se posao bavi, i dnevnik koji raste.
//
// Kad posao završi, ide se na odredište koje je posao sam odredio. Ako ga
// nema, ostaje se ovdje i vidi se ispis.
(function () {
  'use strict';

  var RAZMAK = 1000;

  function nacrtaj(okvir, s) {
    var traka = okvir.querySelector('.posao-traka-crta');
    var sto = okvir.querySelector('.posao-sto');
    var brojka = okvir.querySelector('.posao-brojka');
    var dnevnik = okvir.querySelector('.posao-dnevnik');

    if (traka) {
      if (s.postotak < 0) {
        okvir.classList.add('posao-neodredeno');
        traka.style.width = '100%';
      } else {
        okvir.classList.remove('posao-neodredeno');
        traka.style.width = s.postotak + '%';
      }
    }
    if (sto) { sto.textContent = s.sto || ''; }
    if (brojka) {
      var t = '';
      if (s.ukupno > 0) { t = s.gotovo + ' / ' + s.ukupno; }
      else if (s.postotak >= 0) { t = s.postotak + ' %'; }
      if (s.sekundi > 2) { t += (t ? ' · ' : '') + s.sekundi + ' s'; }
      brojka.textContent = t;
    }
    if (dnevnik && s.redci && s.redci.length) {
      var bio = dnevnik.scrollTop + dnevnik.clientHeight >= dnevnik.scrollHeight - 4;
      dnevnik.textContent = s.redci.join('\n');
      if (bio) { dnevnik.scrollTop = dnevnik.scrollHeight; }
    }
  }

  function zavrsi(okvir, s) {
    okvir.classList.remove('posao-neodredeno');
    okvir.classList.add(s.stanje === 'pao' ? 'posao-pao' : 'posao-gotov');
    var sto = okvir.querySelector('.posao-sto');
    if (sto) { sto.textContent = s.stanje === 'pao' ? (s.greska || 'posao je pao') : (s.sazetak || 'gotovo'); }
    if (s.stanje !== 'pao' && s.odrediste) {
      window.location.href = s.odrediste;
    }
  }

  function prati(okvir) {
    var id = okvir.getAttribute('data-posao');
    if (!id || okvir.dataset.pracen === '1') { return; }
    okvir.dataset.pracen = '1';

    // Posao radi i kad se stranica zatvori; ovo je samo prozor u njega.
    var promasaja = 0;
    (function pitaj() {
      fetch('/poslovi/' + encodeURIComponent(id), { headers: { 'Accept': 'application/json' } })
        .then(function (o) {
          if (o.status === 404) { return { stanje: 'nepoznat' }; }
          if (!o.ok) { throw new Error(o.status); }
          return o.json();
        })
        .then(function (s) {
          if (s.stanje === 'nepoznat') {
            // Posao je istekao ili je poslužitelj u međuvremenu pokrenut
            // iznova; osvježavanje stranice pokaže zatečeno stanje.
            okvir.classList.add('posao-pao');
            var sto = okvir.querySelector('.posao-sto');
            if (sto) { sto.textContent = 'posao se više ne prati — osvježite stranicu da vidite kako je prošlo'; }
            return;
          }
          promasaja = 0;
          nacrtaj(okvir, s);
          if (s.stanje === 'traje') { setTimeout(pitaj, RAZMAK); return; }
          zavrsi(okvir, s);
        })
        .catch(function () {
          // Kratak prekid mreže ne znači da je posao pao; odustaje se tek
          // nakon nekoliko promašaja zaredom.
          promasaja++;
          if (promasaja < 10) { setTimeout(pitaj, RAZMAK * 2); return; }
          var sto = okvir.querySelector('.posao-sto');
          if (sto) { sto.textContent = 'veza s poslužiteljem je prekinuta — osvježite stranicu'; }
        });
    })();
  }

  function pokreni() {
    var okviri = document.querySelectorAll('[data-posao]');
    for (var i = 0; i < okviri.length; i++) { prati(okviri[i]); }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', pokreni);
  } else {
    pokreni();
  }
})();

// Vrata arhive: okvir o zatečenom nizu se puni čim se zna koji je niz.
//
// Naziv datoteke rijetko kaže kojoj letvi pripada, pa čovjek letvu, izvor i
// vrstu upisuje rukom. Poslužitelj dotad ne zna što taj niz već ima u stablu,
// a to je baš ono što treba vidjeti prije upisa. Ovo ga pita čim se polje
// promijeni, bez ponovnog učitavanja stranice — inače bi se gubilo ono što je
// upravo utipkano.
(function () {
  'use strict';

  var POLJA = ['letva', 'sliv', 'izvor', 'velicina', 'vrsta'];

  function pripremi() {
    var forma = document.querySelector('form[action="/administracija/uvoz-niza/upisi"]');
    var okvir = document.getElementById('zatecen-okvir');
    if (!forma || !okvir) { return; }

    var uTijeku = null;
    function pitaj() {
      var letva = forma.elements['letva'];
      if (!letva || !letva.value.trim()) { okvir.innerHTML = ''; return; }
      if (uTijeku) { uTijeku.abort(); }
      var prekid = new AbortController();
      uTijeku = prekid;
      fetch('/administracija/uvoz-niza/zatecen', {
        method: 'POST',
        body: new FormData(forma),
        signal: prekid.signal
      })
        .then(function (o) { return o.ok ? o.text() : ''; })
        .then(function (html) { okvir.innerHTML = html; uTijeku = null; })
        .catch(function () { /* prekinut ili pao — okvir ostaje kakav je */ });
    }

    POLJA.forEach(function (ime) {
      var polje = forma.elements[ime];
      if (polje) { polje.addEventListener('change', pitaj); }
    });
    // Prvi put odmah, za slučaj da naziv datoteke već sve kazuje.
    if (!okvir.innerHTML.trim()) { pitaj(); }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', pripremi);
  } else {
    pripremi();
  }
})();
