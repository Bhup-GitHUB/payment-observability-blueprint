async function sendPayment(scenario) {
  const merchantID = document.getElementById('merchant').value || 'merchant_demo';
  const amount = parseInt(document.getElementById('amount').value, 10) || 1000;
  const currency = document.getElementById('currency').value || 'INR';

  const buttons = document.querySelectorAll('.btn');
  buttons.forEach(b => b.disabled = true);

  const resultCard = document.getElementById('result-card');
  const resultEl = document.getElementById('result');
  resultCard.style.display = 'block';
  resultEl.innerHTML = '<span class="spinner"></span> Processing...';

  const payload = { merchant_id: merchantID, amount, currency, scenario };

  try {
    const start = Date.now();
    const resp = await fetch('/api/payments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    const data = await resp.json();
    const clientMs = Date.now() - start;

    renderResult(data, scenario);
    addFeedItem(data, scenario, clientMs);
  } catch (err) {
    resultEl.innerHTML = `<div class="result-grid">
      <span class="result-label">Error</span>
      <span class="result-value failed">${err.message}</span>
    </div>`;
  } finally {
    buttons.forEach(b => b.disabled = false);
  }
}

function renderResult(data, scenario) {
  const el = document.getElementById('result');
  const traceID = data.trace_id || '';
  const statusClass = data.status || 'failed';

  el.innerHTML = `
    <div class="result-grid">
      <span class="result-label">Payment ID</span>
      <span class="result-value">${data.payment_id || '—'}</span>

      <span class="result-label">Status</span>
      <span class="result-value ${statusClass}">${data.status || '—'}</span>

      <span class="result-label">Duration</span>
      <span class="result-value">${data.duration_ms != null ? data.duration_ms + ' ms' : '—'}</span>

      <span class="result-label">Trace ID</span>
      <span class="result-value">
        ${traceID || '—'}
        ${traceID ? `<button class="copy-btn" onclick="copyTrace('${traceID}')">copy</button>` : ''}
      </span>

      ${data.message ? `
      <span class="result-label">Message</span>
      <span class="result-value">${data.message}</span>
      ` : ''}
    </div>
  `;
}

function addFeedItem(data, scenario, clientMs) {
  const feed = document.getElementById('feed');
  const item = document.createElement('div');
  item.className = 'feed-item';
  const status = data.status || 'failed';
  item.innerHTML = `
    <span class="feed-badge ${status}">${status}</span>
    <span class="feed-id">${data.payment_id || '—'}</span>
    <span style="color:#6e7681">${scenario}</span>
    <span class="feed-dur">${clientMs}ms</span>
  `;
  feed.insertBefore(item, feed.firstChild);
  while (feed.children.length > 50) {
    feed.removeChild(feed.lastChild);
  }
}

function copyTrace(traceID) {
  navigator.clipboard.writeText(traceID).then(() => {
    const btns = document.querySelectorAll('.copy-btn');
    btns.forEach(b => {
      b.textContent = 'copied!';
      setTimeout(() => b.textContent = 'copy', 2000);
    });
  });
}
