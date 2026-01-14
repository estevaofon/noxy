document.addEventListener('DOMContentLoaded', () => {
    const form = document.getElementById('shortenForm');
    const input = document.getElementById('urlInput');
    const resultDiv = document.getElementById('result');
    const shortLink = document.getElementById('shortLink');
    const urlList = document.getElementById('urlList');
    const loading = document.getElementById('loading');

    // Fetch initial list
    fetchUrls();

    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        const originalUrl = input.value;
        if (!originalUrl) return;

        try {
            const res = await fetch('/api/shorten', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ url: originalUrl })
            });

            if (!res.ok) {
                alert('Error shortening URL');
                return;
            }

            const data = await res.json();
            showResult(data.short_url);
            fetchUrls(); // Refresh list
            input.value = '';
        } catch (err) {
            console.error(err);
            alert('Failed to connect to server');
        }
    });

    function showResult(url) {
        // Construct standard URL if relative
        const fullUrl = url.startsWith('http') ? url : window.location.origin + url;
        shortLink.href = fullUrl;
        shortLink.textContent = fullUrl;
        resultDiv.classList.remove('hidden');
    }

    async function fetchUrls() {
        loading.style.display = 'block';
        try {
            const res = await fetch('/api/urls');
            if (res.ok) {
                const data = await res.json();
                renderList(data);
            }
        } catch (err) {
            console.error(err);
        } finally {
            loading.style.display = 'none';
        }
    }

    function renderList(items) {
        urlList.innerHTML = '';
        items.forEach(item => {
            const li = document.createElement('li');
            li.className = 'url-item';

            const shortUrl = window.location.origin + '/' + item.code;

            li.innerHTML = `
                <a href="${shortUrl}" class="code" target="_blank">/${item.code}</a>
                <span class="original" title="${item.original_url}">${item.original_url}</span>
            `;
            urlList.appendChild(li);
        });
    }
});
