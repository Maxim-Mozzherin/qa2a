// Leads Monster: Main Application Controller
document.addEventListener('DOMContentLoaded', () => {
    const state = {
        user: null,
        leads: [],
        total: 0,
        page: 1,
        pageSize: 50,
        totalPages: 1,
        stats: {
            total_count: 0,
            enriched_count: 0,
            high_ticket_count: 0,
            in_pipeline_count: 0
        },
        filters: {
            search: '',
            bill_range: 'all',
            legal_only: false,
            reviews_min: 0,
            rubrics: [],
            status: 'all',
            priority: 'all',
            sort_by: 'avg_bill_val',
            sort_order: 'desc',
            page: 1,
            page_size: 50
        },
        scraperPollingInterval: null,
        activeNotesLead: null
    };

    // DOM Elements
    const elements = {
        // User
        userLoginText: document.getElementById('user-login-text'),
        logoutBtn: document.getElementById('logout-btn'),

        // KPIs
        kpiTotal: document.getElementById('kpi-total'),
        kpiEnriched: document.getElementById('kpi-enriched'),
        kpiHighTicket: document.getElementById('kpi-high-ticket'),
        kpiPipeline: document.getElementById('kpi-pipeline'),

        // Filters
        searchInput: document.getElementById('search-input'),
        legalOnlyToggle: document.getElementById('legal-only-toggle'),
        reviewsMinSelect: document.getElementById('reviews-min-select'),
        statusFilterSelect: document.getElementById('status-filter-select'),
        billPillsContainer: document.getElementById('bill-pills'),
        rubricPillsContainer: document.getElementById('rubric-pills'),
        resetFiltersBtn: document.getElementById('reset-filters-btn'),

        // Table
        leadsTableBody: document.getElementById('leads-table-body'),
        leadsCountBadge: document.getElementById('leads-count-badge'),
        exportExcelBtn: document.getElementById('export-excel-btn'),

        // Pagination
        paginationInfo: document.getElementById('pagination-info'),
        prevPageBtn: document.getElementById('prev-page-btn'),
        nextPageBtn: document.getElementById('next-page-btn'),
        pageSelect: document.getElementById('page-select'),

        // Scraper Modal
        openScraperBtn: document.getElementById('open-scraper-btn'),
        scraperModal: document.getElementById('scraper-modal'),
        closeScraperBtn: document.getElementById('close-scraper-btn'),
        scraperForm: document.getElementById('scraper-form'),
        startScraperSubmitBtn: document.getElementById('start-scraper-submit-btn'),
        stopScraperBtn: document.getElementById('stop-scraper-btn'),
        scraperProgressArea: document.getElementById('scraper-progress-area'),
        scraperStatusText: document.getElementById('scraper-status-text'),
        scraperProgressMetrics: document.getElementById('scraper-progress-metrics'),

        // Notes Modal
        notesModal: document.getElementById('notes-modal'),
        closeNotesBtn: document.getElementById('close-notes-btn'),
        notesLeadName: document.getElementById('notes-lead-name'),
        notesTextarea: document.getElementById('notes-textarea'),
        saveNotesBtn: document.getElementById('save-notes-btn'),

        // Toasts
        toastContainer: document.getElementById('toast-container')
    };

    // Initialize App
    init();

    async function init() {
        try {
            const meRes = await API.getMe();
            state.user = meRes.user;
            if (elements.userLoginText) {
                elements.userLoginText.textContent = state.user.login;
            }
        } catch (err) {
            console.error('Session error:', err);
            return;
        }

        bindEvents();
        loadLeads();
        checkScraperLiveness();
    }

    function bindEvents() {
        // Logout
        if (elements.logoutBtn) {
            elements.logoutBtn.addEventListener('click', async () => {
                try {
                    await API.logout();
                    window.location.href = '/login';
                } catch (e) {
                    window.location.href = '/login';
                }
            });
        }

        // Debounced Search
        let searchDebounceTimer = null;
        if (elements.searchInput) {
            elements.searchInput.addEventListener('input', (e) => {
                clearTimeout(searchDebounceTimer);
                searchDebounceTimer = setTimeout(() => {
                    state.filters.search = e.target.value.trim();
                    state.filters.page = 1;
                    loadLeads();
                }, 300);
            });
        }

        // Legal Entity Toggle
        if (elements.legalOnlyToggle) {
            elements.legalOnlyToggle.addEventListener('change', (e) => {
                state.filters.legal_only = e.target.checked;
                state.filters.page = 1;
                loadLeads();
            });
        }

        // Reviews Select
        if (elements.reviewsMinSelect) {
            elements.reviewsMinSelect.addEventListener('change', (e) => {
                state.filters.reviews_min = parseInt(e.target.value, 10) || 0;
                state.filters.page = 1;
                loadLeads();
            });
        }

        // Status Select Filter
        if (elements.statusFilterSelect) {
            elements.statusFilterSelect.addEventListener('change', (e) => {
                state.filters.status = e.target.value;
                state.filters.page = 1;
                loadLeads();
            });
        }

        // Bill Range Pills
        if (elements.billPillsContainer) {
            elements.billPillsContainer.querySelectorAll('.filter-pill').forEach(pill => {
                pill.addEventListener('click', () => {
                    elements.billPillsContainer.querySelectorAll('.filter-pill').forEach(p => p.classList.remove('active'));
                    pill.classList.add('active');
                    state.filters.bill_range = pill.dataset.range;
                    state.filters.page = 1;
                    loadLeads();
                });
            });
        }

        // Rubrics Category Pills (Multi-select)
        if (elements.rubricPillsContainer) {
            elements.rubricPillsContainer.querySelectorAll('.filter-pill').forEach(pill => {
                pill.addEventListener('click', () => {
                    const rubric = pill.dataset.rubric;
                    if (pill.classList.contains('active')) {
                        pill.classList.remove('active');
                        state.filters.rubrics = state.filters.rubrics.filter(r => r !== rubric);
                    } else {
                        pill.classList.add('active');
                        state.filters.rubrics.push(rubric);
                    }
                    state.filters.page = 1;
                    loadLeads();
                });
            });
        }

        // Reset Filters
        if (elements.resetFiltersBtn) {
            elements.resetFiltersBtn.addEventListener('click', () => {
                state.filters = {
                    search: '',
                    bill_range: 'all',
                    legal_only: false,
                    reviews_min: 0,
                    rubrics: [],
                    status: 'all',
                    priority: 'all',
                    sort_by: 'avg_bill_val',
                    sort_order: 'desc',
                    page: 1,
                    page_size: 50
                };
                if (elements.searchInput) elements.searchInput.value = '';
                if (elements.legalOnlyToggle) elements.legalOnlyToggle.checked = false;
                if (elements.reviewsMinSelect) elements.reviewsMinSelect.value = '0';
                if (elements.statusFilterSelect) elements.statusFilterSelect.value = 'all';

                if (elements.billPillsContainer) {
                    elements.billPillsContainer.querySelectorAll('.filter-pill').forEach(p => {
                        p.classList.toggle('active', p.dataset.range === 'all');
                    });
                }
                if (elements.rubricPillsContainer) {
                    elements.rubricPillsContainer.querySelectorAll('.filter-pill').forEach(p => p.classList.remove('active'));
                }

                loadLeads();
                showToast('Фильтры сброшены', 'info');
            });
        }

        // Table Column Sorting
        document.querySelectorAll('th.sortable').forEach(th => {
            th.addEventListener('click', () => {
                const sortBy = th.dataset.sort;
                if (state.filters.sort_by === sortBy) {
                    state.filters.sort_order = state.filters.sort_order === 'asc' ? 'desc' : 'asc';
                } else {
                    state.filters.sort_by = sortBy;
                    state.filters.sort_order = 'desc';
                }
                loadLeads();
            });
        });

        // Pagination
        if (elements.prevPageBtn) {
            elements.prevPageBtn.addEventListener('click', () => {
                if (state.filters.page > 1) {
                    state.filters.page--;
                    loadLeads();
                }
            });
        }
        if (elements.nextPageBtn) {
            elements.nextPageBtn.addEventListener('click', () => {
                if (state.filters.page < state.totalPages) {
                    state.filters.page++;
                    loadLeads();
                }
            });
        }
        if (elements.pageSelect) {
            elements.pageSelect.addEventListener('change', (e) => {
                state.filters.page = parseInt(e.target.value, 10) || 1;
                loadLeads();
            });
        }

        // Export to Excel
        if (elements.exportExcelBtn) {
            elements.exportExcelBtn.addEventListener('click', () => {
                const exportUrl = API.getExportURL(state.filters);
                showToast('Генерация Excel файла...', 'info');
                window.location.href = exportUrl;
            });
        }

        // Scraper Modal open/close
        if (elements.openScraperBtn) {
            elements.openScraperBtn.addEventListener('click', () => {
                elements.scraperModal.classList.add('active');
                pollScraperStatus();
            });
        }
        if (elements.closeScraperBtn) {
            elements.closeScraperBtn.addEventListener('click', () => {
                elements.scraperModal.classList.remove('active');
            });
        }

        // Quick-chips auto-active handling
        document.querySelectorAll('.quick-chips').forEach(container => {
            container.querySelectorAll('.quick-chip').forEach(chip => {
                chip.addEventListener('click', () => {
                    container.querySelectorAll('.quick-chip').forEach(c => c.classList.remove('active'));
                    chip.classList.add('active');
                });
            });
        });

        // Scraper Form Submit
        if (elements.scraperForm) {
            elements.scraperForm.addEventListener('submit', async (e) => {
                e.preventDefault();
                const city = document.getElementById('scraper-city').value.trim();
                const cityID = document.getElementById('scraper-city-id').value.trim();
                const query = document.getElementById('scraper-query').value.trim();
                const apiKey = document.getElementById('scraper-key').value.trim();
                const maxPages = parseInt(document.getElementById('scraper-pages').value, 10) || 10;
                const minBill = parseFloat(document.getElementById('scraper-min-bill').value) || 0;

                try {
                    elements.startScraperSubmitBtn.disabled = true;
                    await API.startScraper({
                        city,
                        city_id: cityID,
                        query,
                        api_key: apiKey,
                        max_pages: maxPages,
                        min_bill: minBill
                    });
                    showToast('⚡ Парсер 2ГИС успешно запущен!', 'success');
                    startScraperPolling();
                } catch (err) {
                    showToast(err.message, 'error');
                } finally {
                    elements.startScraperSubmitBtn.disabled = false;
                }
            });
        }

        // Stop Scraper
        if (elements.stopScraperBtn) {
            elements.stopScraperBtn.addEventListener('click', async () => {
                try {
                    await API.stopScraper();
                    showToast('Команда остановки отправлена', 'info');
                } catch (err) {
                    showToast(err.message, 'error');
                }
            });
        }

        // Notes Modal close & save
        if (elements.closeNotesBtn) {
            elements.closeNotesBtn.addEventListener('click', () => {
                elements.notesModal.classList.remove('active');
            });
        }
        if (elements.saveNotesBtn) {
            elements.saveNotesBtn.addEventListener('click', async () => {
                if (!state.activeNotesLead) return;
                const newNotes = elements.notesTextarea.value.trim();
                try {
                    elements.saveNotesBtn.disabled = true;
                    await API.updateLead(state.activeNotesLead.id, { notes: newNotes });
                    state.activeNotesLead.notes = newNotes;
                    showToast('Заметки успешно сохранены', 'success');
                    elements.notesModal.classList.remove('active');
                    updateNotesButtonInTable(state.activeNotesLead.id, newNotes);
                } catch (err) {
                    showToast(err.message, 'error');
                } finally {
                    elements.saveNotesBtn.disabled = false;
                }
            });
        }
    }

    // Load Leads with State Filters
    async function loadLeads() {
        renderTableLoading();
        try {
            const data = await API.getLeads(state.filters);
            state.leads = data.leads || [];
            state.total = data.total || 0;
            state.page = data.page || 1;
            state.pageSize = data.page_size || 50;
            state.totalPages = data.total_pages || 1;
            state.stats = data.stats || state.stats;

            renderKPIs();
            renderTable();
            renderPagination();
        } catch (err) {
            renderTableError(err.message);
        }
    }

    function renderKPIs() {
        if (elements.kpiTotal) elements.kpiTotal.textContent = (state.stats.total_count || 0).toLocaleString();
        if (elements.kpiEnriched) elements.kpiEnriched.textContent = (state.stats.enriched_count || 0).toLocaleString();
        if (elements.kpiHighTicket) elements.kpiHighTicket.textContent = (state.stats.high_ticket_count || 0).toLocaleString();
        if (elements.kpiPipeline) elements.kpiPipeline.textContent = (state.stats.in_pipeline_count || 0).toLocaleString();
        if (elements.leadsCountBadge) elements.leadsCountBadge.textContent = `${state.total} заведений`;
    }

    function renderTable() {
        if (!elements.leadsTableBody) return;

        if (state.leads.length === 0) {
            elements.leadsTableBody.innerHTML = `
                <tr>
                    <td colspan="9" class="empty-state">
                        <div style="font-size: 32px; margin-bottom: 8px;">🍽️</div>
                        <div style="font-size: 15px; font-weight: 700; color: #fff;">Заведения не найдены</div>
                        <div style="font-size: 12px; color: #64748b; margin-top: 4px;">Попробуйте смягчить фильтры или запустите парсер 2ГИС</div>
                    </td>
                </tr>
            `;
            return;
        }

        const rows = state.leads.map((lead, idx) => {
            const rowNumber = (state.page - 1) * state.pageSize + idx + 1;
            const priorityClass = (lead.priority || 'medium').toLowerCase();
            const priorityLabel = priorityClass === 'high' ? '🔥 HOT' : (priorityClass === 'medium' ? '⚡ WARM' : '❄️ COLD');

            // Category chips
            const rubricsChips = (lead.rubrics || []).slice(0, 3).map(r => `<span class="chip">${escapeHTML(r)}</span>`).join('');

            // Legal entity block
            let legalBlock = `<span class="legal-none">—</span>`;
            if (lead.legal_name && lead.legal_type && lead.legal_type !== 'NONE') {
                const badgeType = lead.legal_type === 'OOO' ? 'ООО' : (lead.legal_type === 'IP' ? 'ИП' : lead.legal_type);
                const badgeClass = lead.legal_type.toLowerCase();

                let displayName = lead.legal_name;
                if (badgeType === 'ООО' && displayName.startsWith('ООО ')) {
                    displayName = displayName.substring(4);
                } else if (badgeType === 'ИП' && displayName.startsWith('ИП ')) {
                    displayName = displayName.substring(3);
                }

                const rusprofileBtn = lead.rusprofile_url 
                    ? `<a href="${lead.rusprofile_url}" target="_blank" rel="noopener noreferrer" class="rusprofile-btn" title="Проверить в реестре Rusprofile / контрагентах">
                         <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
                         <span>Rusprofile</span>
                       </a>`
                    : '';
                legalBlock = `
                    <div class="legal-cell">
                        <div class="legal-header">
                            <span class="badge-legal-type ${badgeClass}">${badgeType}</span> 
                            <span class="legal-name">${escapeHTML(displayName)}</span>
                        </div>
                        ${rusprofileBtn}
                    </div>
                `;
            }

            // Average bill badge
            const billBadge = lead.avg_bill_val > 0
                ? `<span class="bill-badge">${lead.avg_bill_val.toLocaleString()} ₽</span>`
                : (lead.avg_bill_raw ? `<span style="color:#94a3b8; font-size:11px;">${escapeHTML(lead.avg_bill_raw)}</span>` : `<span style="color:#64748b; font-size:11px;">—</span>`);

            // Contacts: phones + copy button
            let phoneBlock = `<span style="color:#64748b; font-size:11px;">—</span>`;
            if (lead.phones && lead.phones.length > 0) {
                phoneBlock = lead.phones.slice(0, 2).map(ph => `
                    <div class="phone-item">
                        <span>${escapeHTML(ph)}</span>
                        <button class="btn-copy" onclick="copyPhone('${escapeHTML(ph)}')" title="Копировать">📋</button>
                    </div>
                `).join('');
            }

            // Social icons
            const socials = [];
            if (lead.vk_url) socials.push(`<a href="${lead.vk_url}" target="_blank" class="social-icon vk" title="ВКонтакте">VK</a>`);
            if (lead.tg_url) socials.push(`<a href="${lead.tg_url}" target="_blank" class="social-icon tg" title="Telegram">TG</a>`);
            if (lead.website) socials.push(`<a href="${lead.website}" target="_blank" class="social-icon web" title="Сайт">WEB</a>`);
            const socialBlock = socials.length > 0 ? `<div class="social-links">${socials.join('')}</div>` : '';

            // 2GIS link
            const twoGisLink = lead.two_gis_url || (lead.two_gis_id ? `https://2gis.ru/perm/firm/${lead.two_gis_id}` : '#');

            // Rating & Reviews
            const ratingText = lead.rating > 0 ? `⭐ ${lead.rating.toFixed(1)}` : '⭐ —';
            const reviewsText = `<span style="color:#94a3b8; font-size:11px;">(${lead.reviews_count || 0})</span>`;

            // Status select dropdown
            const statusClass = `status-${lead.status || 'new'}`;
            const statusSelect = `
                <select class="status-select ${statusClass}" onchange="changeLeadStatus(${lead.id}, this)">
                    <option value="new" ${lead.status === 'new' ? 'selected' : ''}>Новый</option>
                    <option value="verified_rusprofile" ${lead.status === 'verified_rusprofile' ? 'selected' : ''}>Проверен Rusprofile</option>
                    <option value="contacted" ${lead.status === 'contacted' ? 'selected' : ''}>Взят в контакт</option>
                    <option value="pilot_sent" ${lead.status === 'pilot_sent' ? 'selected' : ''}>Отправлен пилот 1₽</option>
                    <option value="signed_1rub" ${lead.status === 'signed_1rub' ? 'selected' : ''}>Подписан договор</option>
                    <option value="rejected" ${lead.status === 'rejected' ? 'selected' : ''}>Отказ</option>
                </select>
            `;

            // Notes trigger
            const hasNotes = Boolean(lead.notes && lead.notes.trim() !== '');
            const notesBtnText = hasNotes ? escapeHTML(lead.notes) : '+ Заметка';
            const notesBtnClass = hasNotes ? 'notes-btn has-notes' : 'notes-btn';
            const notesBtn = `
                <button id="notes-btn-${lead.id}" class="${notesBtnClass}" onclick="openNotesModal(${lead.id})">
                    ${notesBtnText}
                </button>
            `;

            return `
                <tr id="lead-row-${lead.id}">
                    <td style="font-family:var(--font-mono); color:#64748b; font-size:11px; text-align:center;">
                        <span style="font-weight:700; color:#94a3b8;">${rowNumber}</span>
                        <div style="margin-top:4px;">
                            <span class="badge-priority ${priorityClass}">${priorityLabel}</span>
                        </div>
                    </td>
                    <td>
                        <div class="venue-name">
                            <a href="${twoGisLink}" target="_blank" rel="noopener noreferrer" class="twogis-link" title="Открыть карточку заведения в 2GIS">
                                <span>${escapeHTML(lead.name)}</span>
                                <svg class="ext-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line></svg>
                            </a>
                        </div>
                        <div class="rubric-chips">${rubricsChips}</div>
                    </td>
                    <td>${legalBlock}</td>
                    <td style="text-align:center;">${billBadge}</td>
                    <td>
                        <div class="contacts-cell">
                            ${phoneBlock}
                            ${socialBlock}
                        </div>
                    </td>
                    <td>
                        <div style="font-weight:700; color:#fff; display:flex; align-items:center; gap:5px;">${ratingText} ${reviewsText}</div>
                    </td>
                    <td style="font-size:12px; color:#cbd5e1; min-width:180px; line-height:1.45; word-break:break-word;">
                        ${escapeHTML(lead.address)}
                    </td>
                    <td style="text-align:center;">${statusSelect}</td>
                    <td style="text-align:center;">${notesBtn}</td>
                </tr>
            `;
        }).join('');

        elements.leadsTableBody.innerHTML = rows;
    }

    function renderTableLoading() {
        if (!elements.leadsTableBody) return;
        elements.leadsTableBody.innerHTML = `
            <tr>
                <td colspan="9" class="empty-state">
                    <div style="font-size: 24px; animation: spin 1s linear infinite; display: inline-block;">⏳</div>
                    <div style="font-size: 13px; font-weight: 600; color: #94a3b8; margin-top: 8px;">Загрузка заведений...</div>
                </td>
            </tr>
        `;
    }

    function renderTableError(msg) {
        if (!elements.leadsTableBody) return;
        elements.leadsTableBody.innerHTML = `
            <tr>
                <td colspan="9" class="empty-state" style="color: #fb7185;">
                    <div style="font-size: 24px;">⚠️</div>
                    <div style="font-size: 13px; font-weight: 700; margin-top: 4px;">Ошибка загрузки данных</div>
                    <div style="font-size: 11px; color: #94a3b8; margin-top: 2px;">${escapeHTML(msg)}</div>
                </td>
            </tr>
        `;
    }

    function renderPagination() {
        if (!elements.paginationInfo) return;

        const startItem = state.total === 0 ? 0 : (state.page - 1) * state.pageSize + 1;
        const endItem = Math.min(state.page * state.pageSize, state.total);
        elements.paginationInfo.textContent = `Показано ${startItem}–${endItem} из ${state.total}`;

        if (elements.prevPageBtn) elements.prevPageBtn.disabled = state.page <= 1;
        if (elements.nextPageBtn) elements.nextPageBtn.disabled = state.page >= state.totalPages;

        if (elements.pageSelect) {
            let options = '';
            for (let i = 1; i <= state.totalPages; i++) {
                options += `<option value="${i}" ${i === state.page ? 'selected' : ''}>Стр. ${i} из ${state.totalPages}</option>`;
            }
            elements.pageSelect.innerHTML = options || '<option value="1">Стр. 1 из 1</option>';
        }
    }

    // Scraper Polling
    async function checkScraperLiveness() {
        try {
            const status = await API.getScraperStatus();
            if (status.is_running) {
                startScraperPolling();
            }
        } catch (e) {}
    }

    function startScraperPolling() {
        if (state.scraperPollingInterval) clearInterval(state.scraperPollingInterval);
        pollScraperStatus();
        state.scraperPollingInterval = setInterval(pollScraperStatus, 1500);
    }

    async function pollScraperStatus() {
        try {
            const s = await API.getScraperStatus();
            if (!elements.scraperProgressArea) return;

            if (s.is_running) {
                elements.scraperProgressArea.style.display = 'block';
                if (elements.stopScraperBtn) elements.stopScraperBtn.style.display = 'inline-flex';
                elements.scraperStatusText.innerHTML = `
                    <span style="color:#06b6d4; font-weight:700;">● Парсинг активен:</span> 
                    Стр. ${s.current_page} | Запрос: <b>${escapeHTML(s.query)}</b> (${escapeHTML(s.city)})
                `;
                elements.scraperProgressMetrics.innerHTML = `
                    Найдено: <b>${s.items_found}</b> | Сохранено в базу: <b style="color:#10b981;">${s.items_saved}</b>
                `;
            } else {
                if (elements.stopScraperBtn) elements.stopScraperBtn.style.display = 'none';
                if (s.finished_at && s.finished_at !== '0001-01-01T00:00:00Z') {
                    elements.scraperProgressArea.style.display = 'block';
                    elements.scraperStatusText.innerHTML = `
                        <span style="color:#10b981; font-weight:700;">✓ Парсинг завершен!</span>
                    `;
                    elements.scraperProgressMetrics.innerHTML = `
                        Всего найдено: <b>${s.items_found}</b> | Сохранено: <b style="color:#10b981;">${s.items_saved}</b>
                    `;
                    clearInterval(state.scraperPollingInterval);
                    state.scraperPollingInterval = null;
                    // Reload leads when finished
                    loadLeads();
                } else {
                    elements.scraperProgressArea.style.display = 'none';
                }
            }

            if (s.last_error) {
                elements.scraperStatusText.innerHTML += `<div style="color:#fb7185; font-size:11px; margin-top:4px;">${escapeHTML(s.last_error)}</div>`;
            }
        } catch (e) {}
    }

    // Global Helpers for Table actions
    window.changeLeadStatus = async function(id, selectEl) {
        const newStatus = selectEl.value;
        const oldClass = Array.from(selectEl.classList).find(c => c.startsWith('status-'));
        if (oldClass) selectEl.classList.remove(oldClass);
        selectEl.classList.add(`status-${newStatus}`);

        try {
            await API.updateLead(id, { status: newStatus });
            showToast('Статус обновлен', 'success');
            // Update stats
            loadLeads();
        } catch (err) {
            showToast(err.message, 'error');
            loadLeads();
        }
    };

    window.openNotesModal = function(id) {
        const lead = state.leads.find(l => l.id === id);
        if (!lead) return;

        state.activeNotesLead = lead;
        elements.notesLeadName.textContent = lead.name;
        elements.notesTextarea.value = lead.notes || '';
        elements.notesModal.classList.add('active');
        elements.notesTextarea.focus();
    };

    function updateNotesButtonInTable(id, notes) {
        const btn = document.getElementById(`notes-btn-${id}`);
        if (!btn) return;
        const hasNotes = Boolean(notes && notes.trim() !== '');
        btn.textContent = hasNotes ? notes : '+ Заметка';
        btn.className = hasNotes ? 'notes-btn has-notes' : 'notes-btn';
    }

    window.copyPhone = function(phone) {
        navigator.clipboard.writeText(phone).then(() => {
            showToast(`Номер ${phone} скопирован в буфер!`, 'success');
        }).catch(() => {
            showToast('Не удалось скопировать', 'error');
        });
    };

    function showToast(message, type = 'info') {
        if (!elements.toastContainer) return;
        const toast = document.createElement('div');
        toast.className = `toast ${type}`;
        
        let icon = 'ℹ️';
        if (type === 'success') icon = '✅';
        if (type === 'error') icon = '⚠️';

        toast.innerHTML = `<span>${icon}</span><span>${escapeHTML(message)}</span>`;
        elements.toastContainer.appendChild(toast);

        setTimeout(() => {
            toast.style.opacity = '0';
            toast.style.transform = 'translateX(100%)';
            toast.style.transition = 'all 0.25s ease';
            setTimeout(() => toast.remove(), 250);
        }, 3000);
    }

    function escapeHTML(str) {
        if (!str) return '';
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#039;');
    }
});
