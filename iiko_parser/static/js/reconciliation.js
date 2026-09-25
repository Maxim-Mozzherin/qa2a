// ============================================================================
// АКТ СВЕРКИ С ПОСТАВЩИКАМИ (RECONCILIATION ACT)
// ============================================================================

let selectedReconcileFiles = [];
let registryInvoices = [];
let registryTotalSum = 0;

function initReconciliationEvents() {
    const dropZone = document.getElementById('reconcile-drop-zone');
    const fileInput = document.getElementById('reconcile-file-input');
    const parseBtn = document.getElementById('btn-reconcile-parse');

    // Инициализация дат текущего месяца для ручной сверки
    const dateFromEl = document.getElementById('rec-date-from');
    const dateToEl = document.getElementById('rec-date-to');
    if (dateFromEl && dateToEl && !dateFromEl.value) {
        const now = new Date();
        const year = now.getFullYear();
        const month = String(now.getMonth() + 1).padStart(2, '0');
        const day = String(now.getDate()).padStart(2, '0');
        dateFromEl.value = `${year}-${month}-01`;
        dateToEl.value = `${year}-${month}-${day}`;
    }

    const manualBtn = document.getElementById('btn-reconcile-registry');
    if (manualBtn) {
        manualBtn.addEventListener('click', fetchManualRegistry);
    }

    if (!dropZone || !fileInput) return;

    // Клик по дропзоне
    dropZone.addEventListener('click', (e) => {
        if (e.target !== fileInput) {
            fileInput.click();
        }
    });

    // Drag and Drop
    ['dragenter', 'dragover'].forEach(eventName => {
        dropZone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
            dropZone.classList.add('border-brand-500', 'bg-brand-500/5');
        }, false);
    });

    ['dragleave', 'drop'].forEach(eventName => {
        dropZone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
            dropZone.classList.remove('border-brand-500', 'bg-brand-500/5');
        }, false);
    });

    dropZone.addEventListener('drop', (e) => {
        const dt = e.dataTransfer;
        const files = dt.files;
        if (files && files.length > 0) {
            handleReconcileFilesSelected(files);
        }
    });

    fileInput.addEventListener('change', (e) => {
        if (e.target.files && e.target.files.length > 0) {
            handleReconcileFilesSelected(e.target.files);
        }
    });

    if (parseBtn) {
        parseBtn.addEventListener('click', parseReconciliationAct);
    }
}

function handleReconcileFilesSelected(files) {
    selectedReconcileFiles = Array.from(files);
    const fileNameEl = document.getElementById('reconcile-file-name');
    const parseBtn = document.getElementById('btn-reconcile-parse');

    if (fileNameEl) {
        if (selectedReconcileFiles.length === 1) {
            fileNameEl.innerHTML = `📄 Выбран файл: <b>${escapeHtml(selectedReconcileFiles[0].name)}</b> (${(selectedReconcileFiles[0].size / 1024).toFixed(1)} KB)`;
        } else {
            fileNameEl.innerHTML = `📄 Выбрано файлов: <b>${selectedReconcileFiles.length}</b>`;
        }
        fileNameEl.classList.remove('hidden');
    }

    if (parseBtn) {
        parseBtn.disabled = false;
        parseBtn.classList.remove('opacity-50', 'cursor-not-allowed');
    }
}

async function parseReconciliationAct() {
    const loader = document.getElementById('reconcile-loader');
    const parseBtn = document.getElementById('btn-reconcile-parse');
    const resultsSection = document.getElementById('reconcile-results');
    const errorEl = document.getElementById('reconcile-error');

    if (errorEl) {
        errorEl.classList.add('hidden');
        errorEl.innerText = '';
    }

    const companyId = els.company ? els.company.value : "";
    if (!companyId) {
        alert("Пожалуйста, выберите заведение в верхней панели перед началом сверки");
        return;
    }

    if (!selectedReconcileFiles || selectedReconcileFiles.length === 0) {
        alert("Пожалуйста, прикрепите файл акта сверки (PDF или фото)");
        return;
    }

    let supplierUUID = "";
    const supName = (els.supplierSearch ? els.supplierSearch.value : "").trim().toLowerCase();
    if (supName && typeof iikoSuppliers !== 'undefined' && Array.isArray(iikoSuppliers)) {
        const found = iikoSuppliers.find(s => (s.name || "").toLowerCase() === supName);
        if (found && (found.id || found.uuid)) {
            supplierUUID = found.id || found.uuid;
        }
    }

    const formData = new FormData();
    formData.append('company_id', companyId);
    if (supplierUUID) {
        formData.append('supplier_uuid', supplierUUID);
    }
    selectedReconcileFiles.forEach(f => {
        formData.append('file', f);
    });

    if (loader) loader.classList.remove('hidden');
    if (parseBtn) {
        parseBtn.disabled = true;
        parseBtn.classList.add('opacity-50', 'cursor-not-allowed');
    }
    if (resultsSection) resultsSection.classList.add('hidden');

    try {
        const token = getAuthToken();
        const res = await fetch('api/reconciliation/parse', {
            method: 'POST',
            headers: {
                'Authorization': 'Bearer ' + token
            },
            body: formData
        });

        if (!res.ok) {
            const errText = await res.text();
            throw new Error(errText || `Ошибка сервера (${res.status})`);
        }

        reconciliationData = await res.json();
        currentReconciliationFilter = 'all';

        const summaryGrid = document.querySelector('#reconcile-results .grid');
        if (summaryGrid) summaryGrid.style.display = '';

        const filterFlex = document.querySelector('#reconcile-results .flex.flex-wrap');
        if (filterFlex) filterFlex.style.display = '';

        renderReconciliationSummary();
        renderReconciliationTable();

        if (resultsSection) {
            resultsSection.classList.remove('hidden');
            resultsSection.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
    } catch (err) {
        console.error("Ошибка сверки:", err);
        if (errorEl) {
            errorEl.innerText = "❌ " + err.message;
            errorEl.classList.remove('hidden');
        } else {
            alert("Ошибка сверки: " + err.message);
        }
    } finally {
        if (loader) loader.classList.add('hidden');
        if (parseBtn) {
            parseBtn.disabled = false;
            parseBtn.classList.remove('opacity-50', 'cursor-not-allowed');
        }
    }
}

function filterReconciliation(status) {
    currentReconciliationFilter = status;

    const filterBtns = document.querySelectorAll('.reconcile-filter-btn');
    filterBtns.forEach(btn => {
        if (btn.dataset.status === status) {
            btn.className = "reconcile-filter-btn px-4 py-2 rounded-xl text-xs font-bold transition-all-300 shadow-sm bg-brand-500 text-white";
        } else {
            btn.className = "reconcile-filter-btn px-4 py-2 rounded-xl text-xs font-semibold transition-all-300 bg-[#090d16] text-slate-400 hover:text-white border border-slate-800";
        }
    });

    renderReconciliationTable();
}

function renderReconciliationSummary() {
    if (!reconciliationData) return;

    const matchedCount = (reconciliationData.matched || []).length;
    const mismatchedCount = (reconciliationData.mismatched || []).length;
    const missingCount = (reconciliationData.missing_in_db || []).length;
    const phantomCount = (reconciliationData.phantom_in_db || []).length;

    let matchedSum = 0;
    (reconciliationData.matched || []).forEach(i => matchedSum += (i.act_amount || 0));

    let mismatchedDelta = 0;
    (reconciliationData.mismatched || []).forEach(i => mismatchedDelta += Math.abs(i.diff || 0));

    let missingSum = 0;
    (reconciliationData.missing_in_db || []).forEach(i => missingSum += (i.act_amount || 0));

    let phantomSum = 0;
    (reconciliationData.phantom_in_db || []).forEach(i => phantomSum += (i.db_amount || 0));

    const setCard = (id, count, sumText) => {
        const countEl = document.getElementById(`rec-stat-${id}-count`);
        const sumEl = document.getElementById(`rec-stat-${id}-sum`);
        if (countEl) countEl.innerText = count;
        if (sumEl) sumEl.innerText = sumText;
    };

    setCard('matched', matchedCount, `${matchedSum.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽`);
    setCard('mismatched', mismatchedCount, `Δ ${mismatchedDelta.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽`);
    setCard('missing', missingCount, `${missingSum.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽`);
    setCard('phantom', phantomCount, `${phantomSum.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽`);
}

function renderReconciliationTable() {
    const tbody = document.getElementById('reconcile-tbody');
    if (!tbody || !reconciliationData) return;

    let itemsToRender = [];

    const matched = reconciliationData.matched || [];
    const mismatched = reconciliationData.mismatched || [];
    const missing = reconciliationData.missing_in_db || [];
    const phantom = reconciliationData.phantom_in_db || [];

    if (currentReconciliationFilter === 'all') {
        itemsToRender = [...mismatched, ...missing, ...phantom, ...matched];
    } else if (currentReconciliationFilter === 'matched') {
        itemsToRender = matched;
    } else if (currentReconciliationFilter === 'mismatched') {
        itemsToRender = mismatched;
    } else if (currentReconciliationFilter === 'missing_in_db') {
        itemsToRender = missing;
    } else if (currentReconciliationFilter === 'phantom_in_db') {
        itemsToRender = phantom;
    }

    if (itemsToRender.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" class="p-8 text-center text-slate-500 italic text-xs">
                    Нет записей для выбранного фильтра
                </td>
            </tr>`;
        return;
    }

    let html = '';
    itemsToRender.forEach((it, idx) => {
        let badgeHtml = '';
        let rowClass = '';

        if (it.status === 'matched') {
            badgeHtml = `<span class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">✅ Совпало</span>`;
            rowClass = 'hover:bg-emerald-500/5';
        } else if (it.status === 'mismatched') {
            badgeHtml = `<span class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-amber-500/10 text-amber-400 border border-amber-500/20">⚠️ Расхождение</span>`;
            rowClass = 'bg-amber-500/5 hover:bg-amber-500/10';
        } else if (it.status === 'missing_in_db') {
            badgeHtml = `<span class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-rose-500/10 text-rose-400 border border-rose-500/20">❌ Нет в базе QA2A</span>`;
            rowClass = 'bg-rose-500/5 hover:bg-rose-500/10';
        } else if (it.status === 'phantom_in_db') {
            badgeHtml = `<span class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-indigo-500/10 text-indigo-400 border border-indigo-500/20">👻 Только в нашей базе</span>`;
            rowClass = 'hover:bg-indigo-500/5';
        }

        const actAmountStr = it.act_amount > 0 
            ? `${it.act_amount.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽` 
            : '<span class="text-slate-600">—</span>';

        const dbAmountStr = it.db_amount > 0 
            ? `${it.db_amount.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽` 
            : '<span class="text-slate-600">—</span>';

        let diffStr = '<span class="text-slate-600">0.00 ₽</span>';
        if (it.status === 'mismatched') {
            const diffColor = it.diff > 0 ? 'text-amber-400' : 'text-rose-400';
            const sign = it.diff > 0 ? '+' : '';
            diffStr = `<span class="font-bold ${diffColor}">${sign}${it.diff.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽</span>`;
        } else if (it.status === 'missing_in_db') {
            diffStr = `<span class="font-bold text-rose-400">+${it.act_amount.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽</span>`;
        } else if (it.status === 'phantom_in_db') {
            diffStr = `<span class="font-bold text-indigo-400">-${it.db_amount.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽</span>`;
        }

        html += `
            <tr class="border-b border-slate-800/60 transition-colors ${rowClass}">
                <td class="p-3.5 text-center text-slate-500 text-[11px] font-mono">${idx + 1}</td>
                <td class="p-3.5 whitespace-nowrap">${badgeHtml}</td>
                <td class="p-3.5 font-bold text-white text-xs">${escapeHtml(it.doc_number || "Без номера")}</td>
                <td class="p-3.5 text-slate-400 text-xs font-mono">${escapeHtml(it.doc_date || "—")}</td>
                <td class="p-3.5 text-right font-mono text-xs text-slate-200">${actAmountStr}</td>
                <td class="p-3.5 text-right font-mono text-xs text-slate-200">${dbAmountStr}</td>
                <td class="p-3.5 text-right font-mono text-xs">${diffStr}</td>
            </tr>`;
    });

    tbody.innerHTML = html;
}

async function fetchManualRegistry() {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) {
        alert("Пожалуйста, выберите заведение в верхней панели перед началом сверки");
        return;
    }

    const dateFromEl = document.getElementById('rec-date-from');
    const dateToEl = document.getElementById('rec-date-to');
    const dateFrom = dateFromEl ? dateFromEl.value : "";
    const dateTo = dateToEl ? dateToEl.value : "";

    if (!dateFrom || !dateTo) {
        alert("Пожалуйста, укажите период (дату начала и конца) для ручной сверки");
        return;
    }

    let supplierUUID = "";
    const supName = (els.supplierSearch ? els.supplierSearch.value : "").trim().toLowerCase();
    if (supName && typeof iikoSuppliers !== 'undefined' && Array.isArray(iikoSuppliers)) {
        const found = iikoSuppliers.find(s => (s.name || "").toLowerCase() === supName);
        if (found && (found.id || found.uuid)) {
            supplierUUID = found.id || found.uuid;
        }
    }

    const errorEl = document.getElementById('reconcile-error');
    if (errorEl) {
        errorEl.classList.add('hidden');
        errorEl.innerText = '';
    }

    const btn = document.getElementById('btn-reconcile-registry');
    if (btn) {
        btn.disabled = true;
        btn.innerText = "⏳ Загрузка накладных...";
    }

    try {
        const token = getAuthToken();
        let url = `api/reconciliation/registry?company_id=${companyId}&date_from=${dateFrom}&date_to=${dateTo}`;
        if (supplierUUID) {
            url += `&supplier_uuid=${encodeURIComponent(supplierUUID)}`;
        }

        const res = await fetch(url, {
            headers: { 'Authorization': 'Bearer ' + token }
        });

        if (!res.ok) {
            const errText = await res.text();
            throw new Error(errText || `Ошибка сервера (${res.status})`);
        }

        registryInvoices = await res.json() || [];
        registryTotalSum = registryInvoices.reduce((acc, inv) => acc + (inv.db_amount || 0), 0);

        renderManualRegistry();

        const resultsSection = document.getElementById('reconcile-results');
        if (resultsSection) {
            resultsSection.classList.remove('hidden');
            resultsSection.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
    } catch (err) {
        console.error("Ошибка загрузки реестра для ручной сверки:", err);
        if (errorEl) {
            errorEl.innerText = "❌ " + err.message;
            errorEl.classList.remove('hidden');
        } else {
            alert("Ошибка загрузки: " + err.message);
        }
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.innerText = "📋 Сверить вручную по списку";
        }
    }
}

function renderManualRegistry() {
    const tbody = document.getElementById('reconcile-tbody');
    const resultsSection = document.getElementById('reconcile-results');
    if (!tbody || !resultsSection) return;
    
    const summaryGrid = document.querySelector('#reconcile-results .grid');
    if (summaryGrid) summaryGrid.style.display = 'none';
    const filterFlex = document.querySelector('#reconcile-results .flex.flex-wrap');
    if (filterFlex) filterFlex.style.display = 'none';
    resultsSection.classList.remove('hidden');

    const remainingItems = registryInvoices.filter(i => i !== null);

    if (remainingItems.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" class="p-8 text-center text-slate-500 italic">Все накладные сверены или список пуст! 🎉</td></tr>';
        return;
    }

    tbody.innerHTML = registryInvoices.map((inv, idx) => {
        if (!inv) return '';
        return `
        <tr id="reg-row-${idx}" class="border-b border-slate-800/60 transition-all duration-400 ease-in-out">
            <td class="p-3.5 text-center align-middle">
                <input type="checkbox" class="w-5 h-5 rounded border-slate-700 text-brand-500 focus:ring-brand-500 bg-[#090d16] cursor-pointer" onclick="verifyManualInvoice(${idx}, ${inv.db_amount}, this)">
            </td>
            <td class="p-3.5 whitespace-nowrap align-middle">
                <span class="px-2.5 py-1 rounded-lg text-[10px] font-bold bg-slate-800 text-slate-300">📄 В базе QA2A</span>
            </td>
            <td class="p-3.5 font-bold text-white text-xs align-middle">${escapeHtml(inv.doc_number || "Б/Н")}</td>
            <td class="p-3.5 text-slate-400 text-xs font-mono align-middle">${escapeHtml(inv.doc_date)}</td>
            <td class="p-3.5 text-right font-bold text-brand-400 text-xs align-middle">${inv.db_amount.toLocaleString('ru-RU', { minimumFractionDigits: 2 })} ₽</td>
        </tr>`;
    }).join('');

    tbody.innerHTML += `
        <tr id="reg-total-row" class="bg-[#090d16] font-bold border-t border-slate-700 transition-all duration-300">
            <td colspan="4" class="p-4 text-right text-slate-400 uppercase text-[10px] tracking-widest align-middle">Осталось сверить:</td>
            <td class="p-4 text-right text-brand-400 align-middle" id="reg-total-sum">${registryTotalSum.toLocaleString('ru-RU', { minimumFractionDigits: 2 })} ₽</td>
        </tr>`;
}

window.verifyManualInvoice = function(idx, amount, cbElement) {
    if (!cbElement.checked) return; // Safety check
    
    const row = document.getElementById(`reg-row-${idx}`);
    if (row) {
        // Visual strike-through effect before hiding
        row.classList.add('line-through', 'text-slate-600', 'bg-slate-800/20');
        row.style.opacity = '0.3';
        row.style.transform = 'scale(0.98)';
        
        setTimeout(() => {
            row.style.display = 'none';
        }, 400); // Wait for the transition to finish
    }
    
    // Update totals
    registryTotalSum -= amount;
    if (registryTotalSum < 0.01) registryTotalSum = 0; // Prevent float precision artifacts like -0.00001
    
    const sumEl = document.getElementById('reg-total-sum');
    if (sumEl) {
        sumEl.innerText = registryTotalSum.toLocaleString('ru-RU', { minimumFractionDigits: 2 }) + ' ₽';
    }

    // Track remaining items
    registryInvoices[idx] = null;
    const remaining = registryInvoices.filter(i => i !== null).length;
    if (remaining === 0) {
        setTimeout(() => {
            renderManualRegistry(); // Re-render to show "All matched!" state
        }, 450);
    }
};

window.fetchManualRegistry = fetchManualRegistry;
window.renderManualRegistry = renderManualRegistry;

// Инициализация событий при загрузке DOM
document.addEventListener('DOMContentLoaded', () => {
    initReconciliationEvents();
});
