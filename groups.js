// Groups.js - Modal and API handlers for group management

document.addEventListener('DOMContentLoaded', function() {
  const newGroupBtn = document.getElementById('new-group-btn');
  const groupModal = document.getElementById('groupModal');
  const joinCodeBtn = document.getElementById('open-join-modal-btn');
  const joinCodeModal = document.getElementById('joinCodeModal');
  const closeModals = document.querySelectorAll('.close-modal');
  const cancelBtns = document.querySelectorAll('.cancel-btn');
  const createBtn = document.getElementById('create-btn');
  const joinCodeInput = document.getElementById('joinCodeInput');
  const groupNameInput = document.getElementById('groupNameInput');
  const groupPrivateToggle = document.getElementById('groupPrivateToggle');
  const csrfToken = document.getElementById('csrf-token').value;

  // Open new group modal
  newGroupBtn.addEventListener('click', function() {
    groupModal.classList.add('visible');
    groupNameInput.focus();
  });

  // Open join modal
  joinCodeBtn.addEventListener('click', function() {
    joinCodeModal.classList.add('visible');
    joinCodeInput.focus();
  });

  // Close modals
  closeModals.forEach(function(btn) {
    btn.addEventListener('click', function() {
      groupModal.classList.remove('visible');
      joinCodeModal.classList.remove('visible');
    });
  });

  cancelBtns.forEach(function(btn) {
    btn.addEventListener('click', function() {
      groupModal.classList.remove('visible');
      joinCodeModal.classList.remove('visible');
    });
  });

  // ESC key close
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
      groupModal.classList.remove('visible');
      joinCodeModal.classList.remove('visible');
    }
  });

  // Create group
  createBtn.addEventListener('click', async function() {
    const name = groupNameInput.value.trim();
    const isPrivate = groupPrivateToggle.checked;

    if (!name) {
      alert('Please enter a group name');
      groupNameInput.focus();
      return;
    }

    try {
      const response = await fetch('/api/groups/create', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken
        },
        body: JSON.stringify({ name, isPrivate })
      });

      if (!response.ok) {
        const err = await response.text();
        throw new Error(err || 'Failed to create group');
      }

      const group = await response.json();
      groupModal.classList.remove('visible');
      groupNameInput.value = '';
      groupPrivateToggle.checked = false;
      alert(`Group "${group.name}" created successfully!`);
      location.reload(); // Reload to show new group
    } catch (error) {
      alert('Error creating group: ' + error.message);
    }
  });

  // Join by code
  joinCodeInput.addEventListener('keypress', function(e) {
    if (e.key === 'Enter') joinByCode();
  });

  window.joinByCode = async function() {
    const code = joinCodeInput.value.trim().toUpperCase();

    if (code.length !== 6) {
      alert('Please enter a 6-character invite code');
      joinCodeInput.focus();
      return;
    }

    try {
      const response = await fetch('/api/groups/join-code', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken
        },
        body: JSON.stringify({ code })
      });

      if (!response.ok) {
        const err = await response.text();
        throw new Error(err || 'Invalid code');
      }

      joinCodeModal.classList.remove('visible');
      joinCodeInput.value = '';
      alert('Joined group successfully!');
      location.reload();
    } catch (error) {
      alert('Error: ' + error.message);
    }
  };

  // Load groups
  loadGroups();

  async function loadGroups() {
    try {
      const myGroupsRes = await fetch('/api/groups/my');
      const myGroups = await myGroupsRes.json();
      renderGroups('my-groups-list', myGroups, 'My Groups');

      const publicRes = await fetch('/api/groups/public');
      const publicGroups = await publicRes.json();
      renderGroups('public-groups-list', publicGroups, 'Public Groups');
    } catch (error) {
      console.error('Error loading groups:', error);
      document.getElementById('my-groups-list').innerHTML = '<p>Error loading groups</p>';
    }
  }

  function renderGroups(containerId, groups, title) {
    const container = document.getElementById(containerId);
    if (groups.length === 0) {
      container.innerHTML = '<p>No groups found</p>';
      return;
    }

    let html = `<h3>${title}</h3>`;
    groups.forEach(group => {
      const badge = group.is_private ? 'Private' : 'Public';
      html += `
        <div class="group-card">
          <h4>${escapeHtml(group.name)}</h4>
          <span>${badge}</span>
          <button onclick="window.location.href='/campfire/chat?group=${group.id}'">Chat</button>
          ${group.invite_code ? `<button onclick="copyInvite('${group.invite_code}')">Copy Code</button>` : ''}
        </div>
      `;
    });
    container.innerHTML = html;
  }

  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  window.copyInvite = function(code) {
    navigator.clipboard.writeText(code).then(() => {
      alert('Invite code copied!');
    });
  };
});
