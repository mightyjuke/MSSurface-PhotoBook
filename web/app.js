(() => {
  'use strict';

  const isAdmin = location.pathname.startsWith('/admin');
  document.querySelector(isAdmin ? '#admin' : '#display').hidden = false;
  document.title = isAdmin ? 'Manage PhotoBook' : 'PhotoBook';

  async function api(path, options) {
    const response = await fetch(path, options);
    if (!response.ok) {
      let message = `Request failed (${response.status})`;
      try { message = (await response.json()).error || message; } catch (_) { /* ignore */ }
      throw new Error(message);
    }
    return response.status === 204 ? null : response.json();
  }

  if (isAdmin) initAdmin(); else initDisplay();

  async function initAdmin() {
    const form = document.querySelector('#settings-form');
    const input = document.querySelector('#photo-input');
    const fields = {
      title: document.querySelector('#title'),
      interval: document.querySelector('#interval'),
      transition: document.querySelector('#transition'),
      fit: document.querySelector('#fit'),
      background: document.querySelector('#background'),
      shuffle: document.querySelector('#shuffle'),
      showClock: document.querySelector('#show-clock')
    };
    const selected = new Set();
    const bulkActions = document.querySelector('#bulk-actions');
    const selectedCount = document.querySelector('#selected-count');
    const clearSelection = document.querySelector('#clear-selection');
    const removeSelected = document.querySelector('#remove-selected');
    const confirmOverlay = document.querySelector('#confirm-overlay');
    const confirmMessage = document.querySelector('#confirm-message');
    const confirmCancel = document.querySelector('#confirm-cancel');
    const confirmRemove = document.querySelector('#confirm-remove');
    const softwareVersion = document.querySelector('#software-version');
    const updateState = document.querySelector('#update-state');
    const updateCheckedAt = document.querySelector('#update-checked-at');
    const updateMessage = document.querySelector('#update-message');
    const autoUpdate = document.querySelector('#auto-update');
    const checkUpdate = document.querySelector('#check-update');
    let state;
    let draggedCard = null;
    let savingOrder = false;
    let confirmationResolver = null;

    function closeConfirmation(accepted) {
      if (!confirmationResolver) return;
      const resolve = confirmationResolver;
      confirmationResolver = null;
      confirmOverlay.hidden = true;
      document.body.classList.remove('modal-open');
      resolve(accepted);
    }

    function askToRemove(message) {
      if (confirmationResolver) closeConfirmation(false);
      confirmMessage.textContent = message;
      confirmOverlay.hidden = false;
      document.body.classList.add('modal-open');
      window.setTimeout(() => confirmRemove.focus(), 0);
      return new Promise(resolve => { confirmationResolver = resolve; });
    }

    confirmCancel.addEventListener('click', () => closeConfirmation(false));
    confirmRemove.addEventListener('click', () => closeConfirmation(true));
    confirmOverlay.addEventListener('click', event => {
      if (event.target === confirmOverlay) closeConfirmation(false);
    });
    document.addEventListener('keydown', event => {
      if (event.key === 'Escape' && confirmationResolver) closeConfirmation(false);
    });

    async function refresh() {
      state = await api('/api/admin/state');
      state.photos = Array.isArray(state.photos) ? state.photos : [];
      renderPhotos(state.photos);
      fillSettings(state.config);
      renderStats(state);
    }

    function fillSettings(config) {
      fields.title.value = config.title;
      fields.interval.value = config.intervalSeconds;
      fields.transition.value = config.transition;
      fields.fit.value = config.fit;
      fields.background.value = config.background;
      fields.shuffle.checked = config.shuffle;
      fields.showClock.checked = config.showClock;
      updateOrderHint(config.shuffle);
    }

    function renderStats(current) {
      document.querySelector('#photo-count').textContent = current.photos.length.toLocaleString();
      const bytes = current.photos.reduce((sum, photo) => sum + photo.size, 0);
      document.querySelector('#library-size').textContent = formatBytes(bytes);
      document.querySelector('#interval-summary').textContent = `${current.config.intervalSeconds} sec`;
    }

    function renderUpdateStatus(status) {
      softwareVersion.textContent = status.currentVersion || 'Unknown';
      autoUpdate.checked = status.enabled;
      updateState.textContent = status.state || 'idle';
      updateState.className = `status-chip ${status.state || 'idle'}`;
      updateMessage.textContent = status.message || 'No update status is available.';
      updateCheckedAt.textContent = status.checkedAt
        ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(status.checkedAt))
        : 'Never';
    }

    async function loadUpdateStatus() {
      const status = await api('/api/admin/update');
      renderUpdateStatus(status);
      return status;
    }

    autoUpdate.addEventListener('change', async () => {
      const enabled = autoUpdate.checked;
      autoUpdate.disabled = true;
      try {
        const status = await api('/api/admin/update', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled })
        });
        renderUpdateStatus(status);
        notify(`Automatic updates ${enabled ? 'enabled' : 'disabled'}.`);
      } catch (error) {
        autoUpdate.checked = !enabled;
        notify(error.message, true);
      } finally { autoUpdate.disabled = false; }
    });

    checkUpdate.addEventListener('click', async () => {
      const startingVersion = softwareVersion.textContent;
      checkUpdate.disabled = true;
      checkUpdate.textContent = 'Checking…';
      try {
        renderUpdateStatus(await api('/api/admin/update/check', { method: 'POST' }));
        for (let attempt = 0; attempt < 45; attempt++) {
          await new Promise(resolve => window.setTimeout(resolve, 2000));
          let status;
          try { status = await loadUpdateStatus(); } catch (_) { continue; }
          if (status.currentVersion && status.currentVersion !== startingVersion) {
            location.reload();
            return;
          }
          if (!['queued', 'checking'].includes(status.state)) {
            notify(status.message, status.state === 'error');
            return;
          }
        }
        notify('The update check is still running. Its result will appear here shortly.');
      } catch (error) { notify(error.message, true); }
      finally {
        checkUpdate.disabled = false;
        checkUpdate.textContent = 'Check for updates';
      }
    });

    function renderPhotos(photos) {
      const grid = document.querySelector('#photo-grid');
      const empty = document.querySelector('#empty-library');
      const template = document.querySelector('#photo-template');
      grid.replaceChildren();
      const available = new Set(photos.map(photo => photo.id));
      Array.from(selected).forEach(id => { if (!available.has(id)) selected.delete(id); });
      empty.hidden = photos.length > 0;
      grid.hidden = photos.length === 0;
      photos.forEach(photo => {
        const card = template.content.cloneNode(true);
        const article = card.querySelector('.photo-card');
        article.dataset.photoId = photo.id;
        const img = card.querySelector('img');
        img.src = photo.thumbnailUrl || photo.url;
        img.alt = photo.originalName;
        card.querySelector('.photo-name').textContent = photo.originalName;
        const checkbox = card.querySelector('.photo-select');
        checkbox.checked = selected.has(photo.id);
        article.classList.toggle('selected', checkbox.checked);
        checkbox.addEventListener('change', () => {
          if (checkbox.checked) selected.add(photo.id); else selected.delete(photo.id);
          article.classList.toggle('selected', checkbox.checked);
          updateBulkActions();
        });
        enableReordering(article, card.querySelector('.drag-handle'), grid);
        const button = card.querySelector('.delete-button');
        button.addEventListener('click', async () => {
          if (!await askToRemove(`Remove “${photo.originalName}” from the frame?`)) return;
          button.disabled = true;
          try {
            await api(`/api/admin/photos/${encodeURIComponent(photo.id)}`, { method: 'DELETE' });
            selected.delete(photo.id);
            await refresh();
            notify('Photo removed.');
          } catch (error) { notify(error.message, true); button.disabled = false; }
        });
        grid.appendChild(card);
      });
      updateBulkActions();
    }

    function updateOrderHint(shuffleEnabled) {
      const hint = document.querySelector('#order-hint');
      hint.textContent = shuffleEnabled
        ? 'Shuffle is on. This saved sequence applies when Shuffle is turned off.'
        : 'Drag photos to arrange the slideshow sequence.';
      hint.classList.toggle('warning', shuffleEnabled);
    }

    fields.shuffle.addEventListener('change', () => updateOrderHint(fields.shuffle.checked));

    function enableReordering(article, handle, grid) {
      handle.addEventListener('dragstart', event => {
        draggedCard = article;
        article.classList.add('dragging');
        event.dataTransfer.effectAllowed = 'move';
        event.dataTransfer.setData('text/plain', article.dataset.photoId);
      });
      article.addEventListener('dragover', event => {
        if (!draggedCard || draggedCard === article) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = 'move';
        placeDraggedCard(grid, article, event.clientX, event.clientY);
      });
      article.addEventListener('drop', event => {
        event.preventDefault();
        finishReordering(grid);
      });
      handle.addEventListener('dragend', () => finishReordering(grid));

      handle.addEventListener('pointerdown', event => {
        if (event.pointerType === 'mouse') return;
        event.preventDefault();
        draggedCard = article;
        article.classList.add('dragging');
        handle.setPointerCapture(event.pointerId);
      });
      handle.addEventListener('pointermove', event => {
        if (!draggedCard || event.pointerType === 'mouse') return;
        event.preventDefault();
        const target = document.elementFromPoint(event.clientX, event.clientY)?.closest('.photo-card');
        if (target && target !== draggedCard && target.parentElement === grid) {
          placeDraggedCard(grid, target, event.clientX, event.clientY);
        }
      });
      const finishPointer = event => {
        if (event.pointerType === 'mouse' || !draggedCard) return;
        if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
        finishReordering(grid);
      };
      handle.addEventListener('pointerup', finishPointer);
      handle.addEventListener('pointercancel', finishPointer);
    }

    function placeDraggedCard(grid, target, clientX, clientY) {
      const rect = target.getBoundingClientRect();
      const nearSameRow = Math.abs(clientY - (rect.top + rect.height / 2)) < rect.height * .3;
      const insertBefore = nearSameRow
        ? clientX < rect.left + rect.width / 2
        : clientY < rect.top + rect.height / 2;
      grid.insertBefore(draggedCard, insertBefore ? target : target.nextSibling);
    }

    function finishReordering(grid) {
      if (!draggedCard) return;
      draggedCard.classList.remove('dragging');
      draggedCard = null;
      const ids = Array.from(grid.querySelectorAll('.photo-card'), card => card.dataset.photoId);
      const previous = state.photos.map(photo => photo.id);
      if (ids.every((id, index) => id === previous[index])) return;
      savePhotoOrder(ids);
    }

    async function savePhotoOrder(ids) {
      if (savingOrder) return;
      savingOrder = true;
      try {
        state = await api('/api/admin/photos/order', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ids })
        });
        state.photos = Array.isArray(state.photos) ? state.photos : [];
        renderPhotos(state.photos);
        notify(state.config.shuffle
          ? 'Sequence saved. Turn Shuffle off to play it in this order.'
          : 'Slideshow sequence saved.');
      } catch (error) {
        notify(error.message, true);
        await refresh();
      } finally { savingOrder = false; }
    }

    function updateBulkActions() {
      const count = selected.size;
      bulkActions.hidden = count === 0;
      selectedCount.textContent = `${count} selected`;
    }

    clearSelection.addEventListener('click', () => {
      selected.clear();
      renderPhotos(state.photos);
    });

    removeSelected.addEventListener('click', async () => {
      const count = selected.size;
      if (!count || !await askToRemove(`Remove ${count} selected photo${count === 1 ? '' : 's'} from the frame?`)) return;
      removeSelected.disabled = true;
      try {
        const result = await api('/api/admin/photos', {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ids: Array.from(selected) })
        });
        selected.clear();
        await refresh();
        notify(`${result.removed} photo${result.removed === 1 ? '' : 's'} removed.`);
      } catch (error) { notify(error.message, true); }
      finally { removeSelected.disabled = false; }
    });

    form.addEventListener('submit', async event => {
      event.preventDefault();
      const button = form.querySelector('button[type="submit"]');
      button.disabled = true;
      try {
        const config = {
          title: fields.title.value,
          intervalSeconds: Number(fields.interval.value),
          transition: fields.transition.value,
          fit: fields.fit.value,
          background: fields.background.value,
          shuffle: fields.shuffle.checked,
          showClock: fields.showClock.checked
        };
        await api('/api/admin/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(config) });
        await refresh();
        notify('Frame settings saved.');
      } catch (error) { notify(error.message, true); }
      finally { button.disabled = false; }
    });

    input.addEventListener('change', () => {
      if (!input.files.length) return;
      upload(input.files).finally(() => { input.value = ''; });
    });

    function upload(files) {
      return new Promise(resolve => {
        const data = new FormData();
        Array.from(files).forEach(file => data.append('photos', file));
        const xhr = new XMLHttpRequest();
        const progress = document.querySelector('#upload-progress');
        const bar = progress.querySelector('.progress-track span');
        const copy = progress.querySelector('strong');
        progress.hidden = false;
        xhr.upload.onprogress = event => {
          const value = event.lengthComputable ? Math.round(event.loaded / event.total * 100) : 0;
          bar.style.width = `${value}%`;
          copy.textContent = `${value}%`;
        };
        xhr.onload = async () => {
          progress.hidden = true;
          if (xhr.status >= 200 && xhr.status < 300) {
            await refresh();
            notify(`${files.length} photo${files.length === 1 ? '' : 's'} added.`);
          } else {
            try { notify(JSON.parse(xhr.responseText).error, true); } catch (_) { notify('Upload failed.', true); }
          }
          resolve();
        };
        xhr.onerror = () => { progress.hidden = true; notify('Upload failed. Check the connection.', true); resolve(); };
        xhr.open('POST', '/api/admin/photos');
        xhr.send(data);
      });
    }

    function notify(message, error = false) {
      const notice = document.querySelector('#notice');
      notice.textContent = message;
      notice.classList.toggle('error', error);
      notice.hidden = false;
      clearTimeout(notice.timer);
      notice.timer = setTimeout(() => { notice.hidden = true; }, 4500);
    }

    try { await Promise.all([refresh(), loadUpdateStatus()]); } catch (error) { notify(error.message, true); }
  }

  async function initDisplay() {
    const display = document.querySelector('#display');
    const slides = Array.from(document.querySelectorAll('.slide'));
    const empty = document.querySelector('.empty-frame');
    const clock = document.querySelector('.frame-clock');
    const address = `${location.host}/admin/`;
    document.querySelector('.admin-address').textContent = address;
    let state = null;
    let order = [];
    let position = -1;
    let active = 0;
    let timer;
    let signature = '';
    let loadedVersion = null;

    function shuffled(length) {
      const values = Array.from({ length }, (_, index) => index);
      for (let i = values.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [values[i], values[j]] = [values[j], values[i]];
      }
      return values;
    }

    function applyConfig(config) {
      display.className = `display ${config.transition}`;
      display.style.background = config.background;
      display.style.setProperty('--slide-duration', `${config.intervalSeconds}s`);
      slides.forEach(slide => { slide.style.objectFit = config.fit; });
      clock.hidden = !config.showClock;
      document.title = config.title || 'PhotoBook';
    }

    function next() {
      clearTimeout(timer);
      if (!state || !state.photos.length) return;
      position++;
      if (position >= order.length) {
        order = state.config.shuffle ? shuffled(state.photos.length) : state.photos.map((_, i) => i);
        position = 0;
      }
      const photo = state.photos[order[position]];
      const incoming = slides[1 - active];
      incoming.onload = () => {
        slides[active].classList.remove('active');
        incoming.classList.add('active');
        active = 1 - active;
      };
      incoming.src = photo.displayUrl
        ? `${photo.displayUrl}?fit=${encodeURIComponent(state.config.fit)}`
        : photo.url;
      incoming.alt = photo.originalName;
      timer = setTimeout(next, state.config.intervalSeconds * 1000);
    }

    function clearSlides() {
      clearTimeout(timer);
      order = [];
      position = -1;
      slides.forEach(slide => {
        slide.classList.remove('active');
        slide.removeAttribute('src');
        slide.alt = '';
      });
    }

    async function refresh() {
      try {
        const fresh = await api('/api/frame');
        if (loadedVersion && fresh.version && fresh.version !== loadedVersion) {
          location.reload();
          return;
        }
        loadedVersion = fresh.version || loadedVersion;
        fresh.photos = Array.isArray(fresh.photos) ? fresh.photos : [];
        display.classList.remove('offline');
        applyConfig(fresh.config);
        const nextSignature = fresh.photos.map(photo => photo.id).join(',') + JSON.stringify(fresh.config);
        const photosChanged = !state || fresh.photos.map(photo => photo.id).join(',') !== state.photos.map(photo => photo.id).join(',');
        state = fresh;
        empty.hidden = state.photos.length > 0;
        if (state.photos.length === 0) {
          clearSlides();
        } else if (photosChanged || signature === '') {
          order = state.config.shuffle ? shuffled(state.photos.length) : state.photos.map((_, i) => i);
          position = -1;
          next();
        } else if (nextSignature !== signature) {
          clearTimeout(timer);
          timer = setTimeout(next, state.config.intervalSeconds * 1000);
        }
        signature = nextSignature;
      } catch (_) { display.classList.add('offline'); }
    }

    function updateClock() {
      clock.textContent = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(new Date());
    }

    updateClock();
    setInterval(updateClock, 15000);
    await refresh();
    setInterval(refresh, 15000);
  }

  function formatBytes(bytes) {
    if (!bytes) return '0 MB';
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0)} MB`;
    return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
  }
})();
