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


