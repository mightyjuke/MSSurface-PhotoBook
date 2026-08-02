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
    let updatePollTimer = null;
    let loadedUpdateVersion = null;

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
      const checking = ['queued', 'checking'].includes(status.state);
      softwareVersion.textContent = status.currentVersion || 'Unknown';
      autoUpdate.checked = status.enabled;
      updateState.textContent = status.state || 'idle';
      updateState.className = `status-chip ${status.state || 'idle'}`;
      updateMessage.textContent = status.message || 'No update status is available.';
      updateCheckedAt.textContent = status.checkedAt
        ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(status.checkedAt))
        : 'Never';
      checkUpdate.disabled = checking;
      checkUpdate.textContent = checking ? 'Checking…' : 'Check for updates';
      if (!loadedUpdateVersion) loadedUpdateVersion = status.currentVersion;
      if (!checking && status.currentVersion && status.currentVersion !== loadedUpdateVersion) {
        loadedUpdateVersion = status.currentVersion;
        window.setTimeout(() => location.reload(), 500);
      }
      clearTimeout(updatePollTimer);
      if (checking) updatePollTimer = window.setTimeout(pollUpdateStatus, 2000);
    }

    async function loadUpdateStatus() {
      const status = await api('/api/admin/update');
      renderUpdateStatus(status);
      return status;
    }

    async function pollUpdateStatus() {
      try { await loadUpdateStatus(); }
      catch (_) { updatePollTimer = window.setTimeout(pollUpdateStatus, 2000); }
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
      checkUpdate.disabled = true;
      checkUpdate.textContent = 'Checking…';
      try {
        renderUpdateStatus(await api('/api/admin/update/check', { method: 'POST' }));
      } catch (error) {
        notify(error.message, true);
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
      input.disabled = true;
      upload(input.files).finally(() => { input.value = ''; input.disabled = false; });
    });

    function upload(files) {
      return new Promise(resolve => {
        const data = new FormData();
        Array.from(files).forEach(file => data.append('photos', file));
        const xhr = new XMLHttpRequest();
        const progress = document.querySelector('#upload-progress');
        const bar = progress.querySelector('.progress-track span');
        const label = progress.querySelector('.progress-copy span');
        const valueCopy = progress.querySelector('strong');
        const photoLabel = `${files.length} photo${files.length === 1 ? '' : 's'}`;
        clearTimeout(progress.hideTimer);
        progress.classList.remove('processing');
        label.textContent = `Uploading ${photoLabel}…`;
        valueCopy.textContent = '0%';
        bar.style.width = '0%';
        progress.hidden = false;
        xhr.upload.onprogress = event => {
          const value = event.lengthComputable ? Math.round(event.loaded / event.total * 100) : 0;
          bar.style.width = `${value}%`;
          valueCopy.textContent = `${value}%`;
        };
        xhr.upload.onload = () => {
          progress.classList.add('processing');
          label.textContent = 'Compressing photos and generating display cache…';
          valueCopy.textContent = 'Working…';
          bar.style.width = '35%';
        };
        xhr.onload = async () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            progress.classList.remove('processing');
            label.textContent = 'Refreshing photo library…';
            valueCopy.textContent = '95%';
            bar.style.width = '95%';
            await refresh();
            label.textContent = 'Photos ready';
            valueCopy.textContent = '100%';
            bar.style.width = '100%';
            notify(`${files.length} photo${files.length === 1 ? '' : 's'} added.`);
          } else {
            try { notify(JSON.parse(xhr.responseText).error, true); } catch (_) { notify('Upload failed.', true); }
          }
          progress.hideTimer = window.setTimeout(() => { progress.hidden = true; }, xhr.status >= 200 && xhr.status < 300 ? 500 : 0);
          resolve();
        };
        xhr.onerror = () => {
          progress.classList.remove('processing');
          progress.hidden = true;
          notify('Upload failed. Check the connection.', true);
          resolve();
        };
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
    const clockTime = document.querySelector('.frame-time');
    const clockDate = document.querySelector('.frame-date');
    const weather = document.querySelector('.frame-weather');
    const weatherIcon = document.querySelector('.weather-icon');
    const weatherTemperature = document.querySelector('.weather-temperature');
    const weatherCondition = document.querySelector('.weather-condition');
    const address = `${location.host}/admin/`;
    document.querySelector('.admin-address').textContent = address;
    let state = null;
    let order = [];
    let position = -1;
    let active = 0;
    let timer;
    let signature = '';
    let loadedVersion = null;
    let weatherLastAttempt = 0;

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
      if (config.showClock && Date.now() - weatherLastAttempt >= 15 * 60 * 1000) {
        weatherLastAttempt = Date.now();
        updateWeather();
      }
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
      const now = new Date();
      const twoDigits = value => String(value).padStart(2, '0');
      clockTime.textContent = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(now);
      clockTime.dateTime = now.toISOString();
      clockDate.textContent = `${twoDigits(now.getDate())}/${twoDigits(now.getMonth() + 1)}/${now.getFullYear()}`;
      clockDate.dateTime = `${now.getFullYear()}-${twoDigits(now.getMonth() + 1)}-${twoDigits(now.getDate())}`;
    }

    function describeWeather(code, isDay) {
      if (code === 0) return { icon: isDay ? '☀︎' : '☾', label: 'Clear' };
      if (code === 1) return { icon: isDay ? '☀︎' : '☾', label: 'Mostly clear' };
      if (code === 2) return { icon: '☁︎', label: 'Partly cloudy' };
      if (code === 3) return { icon: '☁︎', label: 'Overcast' };
      if (code === 45 || code === 48) return { icon: '≋', label: 'Foggy' };
      if ([51, 53, 55, 56, 57].includes(code)) return { icon: '☂︎', label: 'Drizzle' };
      if ([61, 63, 65, 66, 67, 80, 81, 82].includes(code)) return { icon: '☂︎', label: 'Rain' };
      if ([71, 73, 75, 77, 85, 86].includes(code)) return { icon: '❄︎', label: 'Snow' };
      if ([95, 96, 99].includes(code)) return { icon: 'ϟ', label: 'Thunderstorm' };
      return { icon: '○', label: 'Current weather' };
    }

    function browserPosition() {
      return new Promise((resolve, reject) => {
        if (!navigator.geolocation) {
          reject(new Error('Location is unavailable'));
          return;
        }
        navigator.geolocation.getCurrentPosition(
          position => resolve({ latitude: position.coords.latitude, longitude: position.coords.longitude }),
          reject,
          { enableHighAccuracy: false, maximumAge: 60 * 60 * 1000, timeout: 10000 }
        );
      });
    }

    async function timeZonePosition() {
      const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
      const city = timeZone.split('/').pop().replace(/_/g, ' ');
      if (!city || city === 'UTC' || timeZone.startsWith('Etc/')) throw new Error('No location fallback');
      const response = await fetch(`https://geocoding-api.open-meteo.com/v1/search?name=${encodeURIComponent(city)}&count=1&language=en&format=json`);
      if (!response.ok) throw new Error('Could not resolve time zone');
      const result = (await response.json()).results?.[0];
      if (!result) throw new Error('Could not resolve time zone');
      return { latitude: result.latitude, longitude: result.longitude };
    }

    async function updateWeather() {
      try {
        let position;
        try { position = await browserPosition(); }
        catch (_) { position = await timeZonePosition(); }
        const params = new URLSearchParams({
          latitude: position.latitude,
          longitude: position.longitude,
          current: 'temperature_2m,weather_code,is_day',
          temperature_unit: 'celsius'
        });
        const response = await fetch(`https://api.open-meteo.com/v1/forecast?${params}`);
        if (!response.ok) throw new Error('Weather is unavailable');
        const current = (await response.json()).current;
        if (!current || !Number.isFinite(current.temperature_2m)) throw new Error('Weather is unavailable');
        const conditions = describeWeather(current.weather_code, current.is_day === 1);
        weatherIcon.textContent = conditions.icon;
        weatherTemperature.textContent = `${Math.round(current.temperature_2m)}°`;
        weatherCondition.textContent = conditions.label;
        weather.hidden = false;
      } catch (_) { /* Keep the last successful reading, if one exists. */ }
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
