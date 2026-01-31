const API_URL = '/api/passwords';

document.addEventListener('DOMContentLoaded', () => {
    loadPasswords();

    document.getElementById('add-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        savePassword();
    });
});

async function loadPasswords() {
    try {
        const res = await fetch(API_URL);
        const passwords = await res.json();

        const grid = document.getElementById('password-list');
        grid.innerHTML = '';

        passwords.forEach(p => {
            const el = createPasswordElement(p);
            grid.appendChild(el);
        });
    } catch (err) {
        console.error('Failed to load passwords', err);
    }
}

async function savePassword() {
    const title = document.getElementById('title').value;
    const username = document.getElementById('username').value;
    const password = document.getElementById('password').value;

    try {
        const res = await fetch(API_URL, {
            method: 'POST',
            body: JSON.stringify({ title, username, password })
        });

        if (res.ok) {
            document.getElementById('add-form').reset();
            loadPasswords();
        } else {
            alert('Failed to save password');
        }
    } catch (err) {
        console.error(err);
        alert('Error saving password');
    }
}

async function deletePassword(id) {
    if (!confirm('Are you sure?')) return;

    try {
        const res = await fetch(`${API_URL}/${id}`, { method: 'DELETE' });
        if (res.ok) {
            loadPasswords();
        }
    } catch (err) {
        console.error(err);
    }
}

function createPasswordElement(p) {
    const card = document.createElement('div');
    card.className = 'card password-item';

    card.innerHTML = `
        <div class="pass-header">
            <div class="pass-title"><i class="fas fa-key"></i> ${escapeHtml(p.title)}</div>
            <div class="pass-actions">
                <button class="delete-btn" onclick="deletePassword(${p.id})">
                    <i class="fas fa-trash"></i>
                </button>
            </div>
        </div>
        
        <div class="pass-detail">
            <i class="fas fa-user"></i>
            <span class="pass-value">${escapeHtml(p.username)}</span>
            <button class="copy-btn" onclick="copyToClipboard('${escapeJs(p.username)}')">
                <i class="far fa-copy"></i>
            </button>
        </div>
        
        <div class="pass-detail">
            <i class="fas fa-lock"></i>
            <span class="pass-value masked" id="pass-${p.id}">${escapeHtml(p.password)}</span>
            <button class="pass-actions" onclick="togglePassVisibility(${p.id})">
                <i class="far fa-eye" id="eye-${p.id}"></i>
            </button>
            <button class="copy-btn" onclick="copyToClipboard('${escapeJs(p.password)}')">
                <i class="far fa-copy"></i>
            </button>
        </div>
        
        <div style="font-size: 0.75rem; color: #666; margin-top: 0.5rem; text-align: right;">
            Added: ${p.created_at}
        </div>
    `;

    return card;
}

function togglePassVisibility(id) {
    const el = document.getElementById(`pass-${id}`);
    const eye = document.getElementById(`eye-${id}`);

    if (el.classList.contains('masked')) {
        el.classList.remove('masked');
        eye.classList.remove('fa-eye');
        eye.classList.add('fa-eye-slash');
    } else {
        el.classList.add('masked');
        eye.classList.remove('fa-eye-slash');
        eye.classList.add('fa-eye');
    }
}

function toggleInputVisibility(id) {
    const el = document.getElementById(id);
    if (el.type === 'password') {
        el.type = 'text';
    } else {
        el.type = 'password';
    }
}

async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        // Could show a toast here
    } catch (err) {
        console.error('Failed to copy', err);
    }
}

function escapeHtml(text) {
    if (!text) return '';
    return text
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

function escapeJs(text) {
    if (!text) return '';
    return text.replace(/'/g, "\\'");
}
