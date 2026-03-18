// groups.js

const CSRF_TOKEN = document.getElementById('csrf-token')?.value || '';

function showError(message) {
  alert('Error: ' + message);
}

function setLoading(btn, isLoading = true) {
  const originalText = btn.dataset.originalText || btn.textContent;
  btn.dataset.originalText = originalText;
  btn.disabled = isLoading;
  btn.textContent = isLoading ? 'Loading...' : originalText;
}

async function apiCall(endpoint, options = {}) {
  const config = {
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': CSRF_TOKEN,
    },
    credentials: 'same-origin',
    ...options
  };

  const response = await fetch(endpoint, config);

  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || `HTTP ${response.status}`);
  }

  return response.json();
}

// ================= MODALS =================

function openModal(modalId) {
  document.getElementById(modalId)?.classList.add('visible');
}

function closeModal(modalId) {
  document.getElementById(modalId)?.classList.remove('visible');
}

function openGroupModal() {
  document.getElementById('groupNameInput').value = '';
  document.getElementById('groupPrivateToggle').checked = false;
  openModal('groupModal');
}

function closeGroupModal() {
  closeModal('groupModal');
}

function openJoinCodeModal() {
  document.getElementById('joinCodeInput').value = '';
  openModal('joinCodeModal');
}

function closeJoinCodeModal() {
  closeModal('joinCodeModal');
}

// ================= ACTIONS =================

async function createNewGroup() {
  const nameInput = document.getElementById('groupNameInput');
  const name = nameInput.value.trim();

  if (!name) {
    nameInput.focus();
    return showError('Please enter a group name');
  }

  const createBtn = document.getElementById('create-btn');
  setLoading(createBtn, true);

  try {
    const isPrivate = document.getElementById('groupPrivateToggle').checked;

    await apiCall('/api/groups/create', {
      method: 'POST',
      body: JSON.stringify({
        name,
        isPublic: !isPrivate
      })
    });

    closeGroupModal();
    await loadMyGroups();
    alert('Group created successfully!');
  } catch (error) {
    showError(error.message);
  } finally {
    setLoading(createBtn, false);
  }
}

async function joinByCode() {
  const codeInput = document.getElementById('joinCodeInput');
  const code = codeInput.value.trim().toUpperCase();

  if (code.length !== 8) {
    codeInput.focus();
    return showError('Join code must be 8 characters long');
  }

  const joinBtn = document.getElementById('join-code-btn');
  setLoading(joinBtn, true);

  try {
    await apiCall('/api/groups/join-code', {
      method: 'POST',
      body: JSON.stringify({ code })
    });

    closeJoinCodeModal();
    await loadMyGroups();
    alert('Successfully joined group!');
  } catch (error) {
    showError(error.message);
  } finally {
    setLoading(joinBtn, false);
  }
}

// ================= RENDER =================

function escapeHtml(text) {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

function renderGroups(groups, containerId) {
  const container = document.getElementById(containerId);

  if (!groups || groups.length === 0) {
    container.innerHTML = '<p class="text-muted">No groups found.</p>';
    return;
  }

  container.innerHTML = groups.map(group => `
    <div class="group-card">
      <div class="group-name">${escapeHtml(group.name)}</div>
      <div>Join Code: <span class="join-code">${group.joinCode || 'N/A'}</span></div>
      <div>Members: ${group.memberCount || 0}</div>
    </div>
  `).join('');
}

// ================= LOADERS =================

async function loadMyGroups() {
  const container = document.getElementById('my-groups-list');
  container.innerHTML = '<p>Loading your groups...</p>';

  try {
    const groups = await apiCall('/api/groups/my');
    renderGroups(groups, 'my-groups-list');
  } catch {
    container.innerHTML = '<p>Failed to load groups.</p>';
  }
}

async function loadPublicGroups() {
  const container = document.getElementById('public-groups-list');
  container.innerHTML = '<p>Loading public groups...</p>';

  try {
    const groups = await apiCall('/api/groups/public');
    renderGroups(groups, 'public-groups-list');
  } catch {
    container.innerHTML = '<p>Failed to load public groups.</p>';
  }
}

async function loadInterestsPeople() {
  const container = document.getElementById('interests-people-list');
  container.innerHTML = '<p>Loading...</p>';

  try {
    const users = await apiCall('/api/users/by-interests');

    container.innerHTML = users.map(user => `
      <div class="group-card">
        <div class="group-name">${escapeHtml(user.name || user.email)}</div>
        <p>Common interests match</p>
      </div>
    `).join('');
  } catch {
    container.innerHTML = '<p>No similar users found.</p>';
  }
}

// ================= INIT =================

document.addEventListener('DOMContentLoaded', () => {

  // OPEN MODALS
  document.getElementById('new-group-btn')?.addEventListener('click', openGroupModal);
  document.getElementById('open-join-modal-btn')?.addEventListener('click', openJoinCodeModal);

  // CLOSE MODALS
  document.querySelectorAll('#groupModal .close-modal, #groupModal .cancel-btn')
    .forEach(el => el.addEventListener('click', closeGroupModal));

  document.querySelectorAll('#joinCodeModal .close-modal, #joinCodeModal .cancel-btn')
    .forEach(el => el.addEventListener('click', closeJoinCodeModal));

  // ACTION BUTTONS
  document.getElementById('create-btn')?.addEventListener('click', createNewGroup);
  document.getElementById('join-code-btn')?.addEventListener('click', joinByCode);

  // BACKDROP CLICK CLOSE
  document.querySelectorAll('.modal').forEach(modal => {
    modal.addEventListener('click', (e) => {
      if (e.target === modal) modal.classList.remove('visible');
    });
  });

  // AUTO UPPERCASE
  document.getElementById('joinCodeInput')?.addEventListener('input', function () {
    this.value = this.value.toUpperCase();
  });

  // LOAD DATA
  loadMyGroups();
  loadPublicGroups();
  loadInterestsPeople();
});