async function executeParseWithFiles(fileObjs) {
    if (!fileObjs || fileObjs.length === 0) {
        alert("����������, �������� ���� ��������� (PDF ��� ����)");
        return;
    }

    const companyId = els.company.value;
    if (!companyId) {
        alert("Пожалуйста, сначала выберите активное заведение!");
        return;
    }

    const formData = new FormData();
    for(let i=0; i<fileObjs.length; i++) { formData.append('pdf', fileObjs[i], fileObjs[i].name); }
    formData.append('company_id', companyId);

    els.btnParse.disabled = true;
    els.loaderParse.classList.remove('hidden');
    els.resSection.classList.add('hidden');

    try {
        const res = await fetch('api/parse?token=' + getAuthToken(), {
            method: 'POST',
            body: formData
        });

        if (!res.ok) throw new Error(await res.text());

        currentDocData = await res.json();
        renderTable(currentDocData);
        els.resSection.classList.remove('hidden');
    } catch (err) {
        alert("❌ Ошибка парсинга AI: " + err.message);
    } finally {
        els.btnParse.disabled = false;
        els.loaderParse.classList.add('hidden');
    }
}
async function executeAppendParseWithFiles(fileObjs) {
    if (!fileObjs || fileObjs.length === 0) return;
    if (!currentDocData) {
        alert("Сначала загрузите основную накладную!");
        return;
    }

    const companyId = els.company.value;
    const formData = new FormData();
    for (let i = 0; i < fileObjs.length; i++) {
        formData.append('pdf', fileObjs[i], fileObjs[i].name);
    }
    formData.append('company_id', companyId);
    if (currentCustomPrompt) {
        formData.append('prompt', currentCustomPrompt);
    }

    els.btnAddPage.disabled = true;
    els.loaderAddPage.classList.remove('hidden');

    try {
        const res = await fetch('api/parse?token=' + getAuthToken(), {
            method: 'POST',
            body: formData
        });

        if (!res.ok) throw new Error(await res.text());

        const appendData = await res.json();
        if (appendData && appendData.items && appendData.items.length > 0) {
            currentDocData.items = currentDocData.items.concat(appendData.items);
            renderTable(currentDocData);
        } else {
            alert("На добавленной странице товаров не найдено.");
        }
    } catch (err) {
        alert("Ошибка распознавания новой страницы: " + err.message);
    } finally {
        els.btnAddPage.disabled = false;
        els.loaderAddPage.classList.add('hidden');
        els.addFile.value = "";
    }
}
function updateFooterTotals() {
    if (!currentDocData || !currentDocData.items) return;
    let totalWithNds = 0;
    let totalWithoutNds = 0;
    currentDocData.items.forEach(item => {
        let q = parseFloat(item.quantity) || 0;
        let p = parseFloat(item.price) || 0;
        let nds = parseFloat(item.nds_percent) || 0;
        let sWithNds = (typeof item.sum === 'number' && !isNaN(item.sum)) ? item.sum : (q * p);
        let sumWithoutNds = sWithNds / (1 + nds/100);
        totalWithNds += sWithNds;
        totalWithoutNds += sumWithoutNds;
    });
    const elWithNds = document.getElementById('footer-total-with-nds');
    const elWithoutNds = document.getElementById('footer-total-without-nds');
    if (elWithNds) elWithNds.innerText = totalWithNds.toFixed(2) + " ₽";
    if (elWithoutNds) elWithoutNds.innerText = totalWithoutNds.toFixed(2) + " ₽";
}
window.swapParties = function() {
    if (!currentDocData) return;
    const temp = currentDocData.shipper;
    currentDocData.shipper = currentDocData.consignee;
    currentDocData.consignee = temp;
    // Update DOM
    const sEl = document.getElementById('res-shipper');
    const cEl = document.getElementById('res-consignee');
    if (sEl) sEl.value = currentDocData.shipper || '';
    if (cEl) cEl.value = currentDocData.consignee || '';
};

function executeAddEmptyRow() {
    if (!currentDocData) {
        currentDocData = {
            vendor_name: "Ручной ввод",
            doc_number: "Б/Н",
            doc_date: new Date().toISOString().split('T')[0],
            consignee: "",
            shipper: "",
            items: []
        };
        document.getElementById('results-section').classList.remove('hidden');
    }
    
    currentDocData.items.push({
        name: "Новый товар",
        clean_category: "Без категории",
        brand: "",
        quantity: 1.0,
        price: 0.0,
        sum: 0.0,
        sum_without_nds: 0.0,
        nds_percent: 0.0,
        mapped_uuid: "",
        mapped_name: "",
        multiplier: 1.0,
        is_ai_guessed: false,
        is_weight_changed: false,
        ai_multiplier: 1.0,
        ai_tip: "Ручной ввод"
    });
    renderTable(currentDocData);
}
function renderTable(data) {
    els.resVendor.innerText = data.vendor_name || "Не определен";
    els.resDocnum.innerText = data.doc_number || "Б/Н";
    
    if (els.resDocdate) {
        els.resDocdate.value = data.doc_date || "";
    }

    if (els.resConsignee) {
        els.resConsignee.value = data.consignee || "";
        els.resConsignee.title = data.consignee || "";
    }
    if (els.resShipper) {
        els.resShipper.value = data.shipper || "";
        els.resShipper.title = data.shipper || "";
    }

    const companyId = els.company.value;

    if (data.mapped_store_uuid && els.store) {
        els.store.value = data.mapped_store_uuid;
    } else {
        const savedStore = localStorage.getItem(`saved_store_${companyId}`);
        if (savedStore) els.store.value = savedStore;
    }

    if (data.mapped_supplier_uuid) {
        const cleanSupUuid = (data.mapped_supplier_uuid || '').toLowerCase().trim();
        const foundSup = iikoSuppliers.find(s => (s.uuid || '').toLowerCase().trim() === cleanSupUuid);
        if (foundSup) {
            els.supplierSearch.value = foundSup.name;
        }
    } else {
        const savedSupplierName = localStorage.getItem(`saved_supplier_name_${companyId}`);
        if (savedSupplierName) els.supplierSearch.value = savedSupplierName;
    }

    els.tbody.innerHTML = '';

    let totalWithNds = 0;
    let totalWithoutNds = 0;

    data.items.forEach((item, idx) => {
        let prefillName = '';
        if (item.mapped_uuid) {
            const cleanUuid = (item.mapped_uuid || '').toLowerCase().trim();
            const found = iikoCatalog.find(c => (c.uuid || '').toLowerCase().trim() === cleanUuid);
            if (found) {
                prefillName = found.name;
            } else if (item.mapped_name) {
                prefillName = item.mapped_name;
            } else {
                prefillName = item.mapped_uuid;
            }
        } else if (item.mapped_name) {
            prefillName = item.mapped_name;
        }

        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle";

        const aiQty = parseFloat(item.quantity) || 0;
        const initMult = parseFloat(item.multiplier) || 1.0;
        const initFinalQty = aiQty * initMult;

        const isAi = item.is_ai_guessed;
        const isWarning = item.is_weight_changed;

        let multClass = "border-slate-800 bg-[#090d16] text-white focus:border-brand-500 focus:bg-[#111827]";
        let multBadge = "";

        if (isWarning) {
            multClass = "border-amber-500/80 bg-amber-500/10 text-amber-300 font-semibold focus:border-amber-500 focus:ring-2 focus:ring-amber-500/20";
            multBadge = `
                <div class="text-[9px] text-amber-300 font-semibold mt-1.5 text-center bg-amber-500/10 border border-amber-500/20 rounded px-1.5 py-0.5 inline-block">
                    ⚠️ Вес изменился! (ИИ: ${item.ai_multiplier} | Было: ${item.history_multiplier})
                </div>`;
        } else if (isAi) {
            multClass = "border-teal-500/80 bg-teal-500/10 text-teal-300";
            multBadge = `<div class="text-[9px] text-teal-400 font-semibold mt-1 text-center">✨ ИИ веса</div>`;
        }

        const aiTipHtml = (item.ai_tip && item.ai_tip !== "Обычный товар")
            ? `<div class="text-[10px] text-brand-400 font-medium mt-1 bg-brand-500/5 border border-brand-500/10 rounded px-2 py-0.5 inline-block">💡 ${escapeHtml(item.ai_tip)}</div>`
            : "";

        const finalSumWithNds = parseFloat(item.sum) || 0;
        const ndsPercent = parseFloat(item.nds_percent) || 0;

        let sumWithoutNds = parseFloat(item.sum_without_nds);
        if (isNaN(sumWithoutNds) || sumWithoutNds <= 0) {
            sumWithoutNds = finalSumWithNds;
            if (ndsPercent > 0) {
                sumWithoutNds = finalSumWithNds / (1.0 + (ndsPercent / 100.0));
            }
        }

        totalWithNds += finalSumWithNds;
        totalWithoutNds += sumWithoutNds;

        const aiCat = item.clean_category || "";
        const catOptions = ["Мясо и птица", "Рыба и морепродукты", "Овощи и фрукты", "Молочные продукты", "Бакалея", "Консервы", "Напитки", "Хозяйственные товары", "Прочее", "Без категории"];
        if (aiCat && !catOptions.includes(aiCat)) catOptions.push(aiCat);
        
        let catOptionsHtml = catOptions.map(c => `<option value="${escapeHtml(c)}" ${c === aiCat ? 'selected' : ''}>${escapeHtml(c)}</option>`).join('');

        tr.innerHTML = `
            <td class="px-2 py-3 text-center text-slate-500 font-semibold border-r border-slate-800/40 align-middle">${idx + 1}</td>
            <td class="px-2 py-3 align-middle">
                <textarea rows="2" class="w-full bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 font-bold text-slate-100 tracking-tight text-xs pb-1 transition-all resize-none overflow-y-auto leading-snug" oninput="currentDocData.items[${idx}].name = this.value;">${escapeHtml(item.name)}</textarea>
                ${aiTipHtml}
            </td>
            <td class="px-2 py-3 align-middle">
                <select class="clean-category-select w-full bg-[#090d16] border border-slate-800 text-white rounded-lg p-2 text-[10px] outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300">
                    <option value="">Выберите...</option>
                    ${catOptionsHtml}
                </select>
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <input type="text" inputmode="decimal" class="w-12 bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 text-center font-extrabold text-slate-300 text-xs pb-1 transition-all ai-qty-input" value="${aiQty}">
            </td>
            <td class="px-2 py-3 text-center border-r border-slate-800/40 align-middle">
                <div class="flex items-center justify-center gap-1"><input type="text" inputmode="decimal" class="w-14 bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 text-center font-semibold text-white text-xs pb-1 transition-all ai-price-input" value="${item.price.toFixed(2)}"> <span class="text-[10px]">₽</span></div>
                <div class="text-[10px] text-slate-500 mt-1 flex items-center justify-center gap-1"><input type="text" inputmode="decimal" class="w-16 bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 text-center font-bold text-slate-300 text-[11px] pb-1 transition-all ai-sum-input" value="${finalSumWithNds.toFixed(2)}"> <span class="text-[10px]">₽ (всего)</span></div>
                <div class="text-[9px] text-brand-400 font-semibold mt-1 bg-brand-500/10 rounded px-1.5 py-0.5 inline-block">НДС ${item.nds_percent}%</div>
            </td>
            <td class="px-2 py-3 align-middle">
                <input type="text" list="iiko-catalog-list" class="iiko-search w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" 
                    value="${escapeHtml(prefillName)}" placeholder="Начните вводить или вставьте UUID...">
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <input type="text" inputmode="decimal" oninput="this.value = this.value.replace(/[^0-9.,]/g, '');" class="iiko-final-qty w-20 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 text-center ${multClass} font-bold" 
                    value="${initFinalQty.toFixed(3)}">
                ${multBadge}
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <button class="bg-rose-500/10 text-rose-400 border border-rose-500/20 hover:bg-rose-500/20 transition-all-300 px-2.5 py-1.5 rounded-lg text-[10px] font-bold" onclick="deleteInvoiceItem(${idx})">
                    ✕
                </button>
            </td>
        `;

        const finalQtyInput = tr.querySelector('.iiko-final-qty');
        const aiQtyInput = tr.querySelector('.ai-qty-input');
        const aiPriceInput = tr.querySelector('.ai-price-input');
        const sumInput = tr.querySelector('.ai-sum-input');

        // When Final Qty changes -> Recalculate Multiplier and update model
        const recalcFromFinalQty = () => {
            let finalQ = parseFloat(finalQtyInput.value.replace(',', '.')) || 0;
            let q = parseFloat(aiQtyInput.value.replace(',', '.')) || 0;
            let p = parseFloat(aiPriceInput.value.replace(',', '.')) || 0;
            let m = q > 0 ? (finalQ / q) : 1.0;
            let sWithNds = parseFloat(sumInput.value.replace(',', '.')) || (q * p);

            updateDataModel(q, p, m, sWithNds);
        };

        // When Qty or Price changes -> Recalculate Sum and Final Qty
        const recalcFromQtyPrice = () => {
            let q = parseFloat(aiQtyInput.value.replace(',', '.')) || 0;
            let p = parseFloat(aiPriceInput.value.replace(',', '.')) || 0;
            let m = (currentDocData.items[idx] && typeof currentDocData.items[idx].multiplier === 'number') ? currentDocData.items[idx].multiplier : 1.0;
            
            let sWithNds = q * p;
            sumInput.value = sWithNds.toFixed(2);
            if (finalQtyInput) {
                finalQtyInput.value = (q * m).toFixed(3);
            }
            
            updateDataModel(q, p, m, sWithNds);
        };

        // When Sum changes -> Recalculate Price
        const recalcFromSum = () => {
            let q = parseFloat(aiQtyInput.value.replace(',', '.')) || 0;
            let sWithNds = parseFloat(sumInput.value.replace(',', '.')) || 0;
            let m = (currentDocData.items[idx] && typeof currentDocData.items[idx].multiplier === 'number') ? currentDocData.items[idx].multiplier : 1.0;
            
            let p = q > 0 ? sWithNds / q : 0;
            aiPriceInput.value = p.toFixed(4);
            
            updateDataModel(q, p, m, sWithNds);
        };

        const updateDataModel = (q, p, m, sum) => {
            currentDocData.items[idx].quantity = q;
            currentDocData.items[idx].price = p;
            currentDocData.items[idx].multiplier = m;
            currentDocData.items[idx].sum = sum;
            
            let nds = parseFloat(currentDocData.items[idx].nds_percent) || 0;
            currentDocData.items[idx].sum_without_nds = sum / (1 + nds/100);
            
            updateFooterTotals();
        };

        if (finalQtyInput) finalQtyInput.addEventListener('input', recalcFromFinalQty);
        if (aiQtyInput) aiQtyInput.addEventListener('input', recalcFromQtyPrice);
        if (aiPriceInput) aiPriceInput.addEventListener('input', recalcFromQtyPrice);
        if (sumInput) sumInput.addEventListener('input', recalcFromSum);

        els.tbody.appendChild(tr);
    });

    if (data.items.length > 0) {
        const totalTr = document.createElement('tr');
        totalTr.className = "bg-[#090d16] font-semibold border-t border-slate-800 text-slate-300 text-xs align-middle";
        totalTr.innerHTML = `
            <td class="p-4 text-center text-slate-500">∑</td>
            <td class="p-4 text-left uppercase text-[10px] font-semibold tracking-wider text-slate-500" colspan="2">Итого накладная:</td>
            <td class="p-4 text-left align-middle border-r border-slate-800/40" colspan="4">
                <span class="text-slate-500 font-normal">Без НДС:</span> 
                <span id="footer-total-without-nds" class="text-slate-200 font-semibold mr-6">${totalWithoutNds.toFixed(2)} ₽</span>
                <span class="text-slate-500 font-normal">С НДС:</span> 
                <span id="footer-total-with-nds" class="text-brand-400 font-semibold">${totalWithNds.toFixed(2)} ₽</span>
            </td>
            <td></td>
        `;
        els.tbody.appendChild(totalTr);
    }
}
function deleteInvoiceItem(idx) {
    if (!currentDocData || !currentDocData.items) return;

    const itemName = currentDocData.items[idx].name;
    if (confirm(`Вычеркнуть позицию из накладной?\n\n"${itemName}"`)) {
        currentDocData.items.splice(idx, 1);
        renderTable(currentDocData);
    }
}

// ============================================================================
// АРХИВ НАКЛАДНЫХ (ИСТОРИЯ ЗАКУПОК)
// ============================================================================

let archiveInvoices = [];
let currentEditingInvoice = null;
let currentEditingItems = [];

window.openInvoiceArchive = function() {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) {
        alert("⚠️ Пожалуйста, сначала выберите заведение в шапке страницы!");
        return;
    }

    const modal = document.getElementById('invoice-archive-modal');
    if (modal) {
        modal.classList.remove('hidden');
        backToArchiveInvoicesList();
        loadArchiveInvoices();
    }
};

window.closeInvoiceArchive = function() {
    const modal = document.getElementById('invoice-archive-modal');
    if (modal) modal.classList.add('hidden');
};

window.backToArchiveInvoicesList = function() {
    const invoicesView = document.getElementById('archive-invoices-view');
    const itemsView = document.getElementById('archive-items-view');
    if (invoicesView) invoicesView.classList.remove('hidden');
    if (itemsView) itemsView.classList.add('hidden');
    currentEditingInvoice = null;
    currentEditingItems = [];
};

window.loadArchiveInvoices = async function() {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) return;

    const tbody = document.getElementById('archive-invoices-tbody');
    if (!tbody) return;

    tbody.innerHTML = `
        <tr>
            <td colspan="6" class="p-8 text-center text-slate-400">
                <div class="inline-block w-5 h-5 border-2 border-brand-500 border-t-transparent rounded-full animate-spin mr-2 align-middle"></div>
                Загрузка архива накладных...
            </td>
        </tr>`;

    try {
        const res = await fetch(`api/history/invoices?company_id=${companyId}`, {
            headers: {
                'Authorization': getAuthToken()
            }
        });
        if (!res.ok) {
            const errTxt = await res.text();
            throw new Error(errTxt || "Ошибка загрузки списка");
        }
        archiveInvoices = await res.json() || [];
        renderArchiveInvoicesTable(archiveInvoices);
    } catch (e) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" class="p-8 text-center text-rose-400">
                    ❌ Ошибка загрузки: ${escapeHtml(e.message)}
                </td>
            </tr>`;
    }
};

function renderArchiveInvoicesTable(invoices) {
    const tbody = document.getElementById('archive-invoices-tbody');
    if (!tbody) return;
    tbody.innerHTML = "";

    if (!invoices || invoices.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" class="p-8 text-center text-slate-500">
                    📭 В архиве заведения пока нет проведенных накладных
                </td>
            </tr>`;
        return;
    }

    invoices.forEach((inv, idx) => {
        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827] transition-colors border-b border-slate-800/40";
        tr.innerHTML = `
            <td class="px-4 py-3.5 font-bold text-white">${escapeHtml(inv.invoice_number || '—')}</td>
            <td class="px-4 py-3.5 text-slate-300">${escapeHtml(inv.invoice_date || '—')}</td>
            <td class="px-4 py-3.5 text-slate-300 truncate max-w-[280px]" title="${escapeHtml(inv.supplier_name)}">${escapeHtml(inv.supplier_name || '—')}</td>
            <td class="px-4 py-3.5 text-center text-slate-400 font-semibold">${inv.items_count}</td>
            <td class="px-4 py-3.5 text-right font-bold text-brand-400">${inv.total_sum ? inv.total_sum.toFixed(2) : '0.00'} ₽</td>
            <td class="px-4 py-3.5 text-center">
                <button type="button" class="bg-brand-500/10 hover:bg-brand-500/20 text-brand-400 border border-brand-500/30 font-semibold px-3 py-1.5 rounded-lg text-xs transition-all-300"
                    onclick='editArchiveInvoice(${JSON.stringify(inv.invoice_number)}, ${JSON.stringify(inv.invoice_date)}, ${JSON.stringify(inv.supplier_name)})'>
                    ✏️ Редактировать
                </button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

window.filterArchiveInvoices = function() {
    const q = (document.getElementById('archive-search-input')?.value || '').toLowerCase().trim();
    if (!q) {
        renderArchiveInvoicesTable(archiveInvoices);
        return;
    }
    const filtered = archiveInvoices.filter(inv => 
        (inv.invoice_number && inv.invoice_number.toLowerCase().includes(q)) ||
        (inv.supplier_name && inv.supplier_name.toLowerCase().includes(q)) ||
        (inv.invoice_date && inv.invoice_date.toLowerCase().includes(q))
    );
    renderArchiveInvoicesTable(filtered);
};

window.editArchiveInvoice = async function(invoiceNumber, invoiceDate, supplierName) {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) return;

    currentEditingInvoice = {
        invoice_number: invoiceNumber,
        invoice_date: invoiceDate,
        supplier_name: supplierName
    };

    const invoicesView = document.getElementById('archive-invoices-view');
    const itemsView = document.getElementById('archive-items-view');
    if (invoicesView) invoicesView.classList.add('hidden');
    if (itemsView) itemsView.classList.remove('hidden');

    const docnumEl = document.getElementById('archive-edit-docnum');
    const docdateEl = document.getElementById('archive-edit-docdate');
    const vendorEl = document.getElementById('archive-edit-vendor');
    if (docnumEl) docnumEl.innerText = `Накладная № ${invoiceNumber}`;
    if (docdateEl) docdateEl.innerText = `от ${invoiceDate}`;
    if (vendorEl) vendorEl.innerText = `Поставщик: ${supplierName}`;

    const tbody = document.getElementById('archive-items-tbody');
    if (tbody) {
        tbody.innerHTML = `
            <tr>
                <td colspan="7" class="p-8 text-center text-slate-400">
                    <div class="inline-block w-5 h-5 border-2 border-brand-500 border-t-transparent rounded-full animate-spin mr-2 align-middle"></div>
                    Загрузка позиций накладной...
                </td>
            </tr>`;
    }

    try {
        const res = await fetch(`api/history/invoice-items?company_id=${companyId}&invoice_number=${encodeURIComponent(invoiceNumber)}`, {
            headers: { 'Authorization': getAuthToken() }
        });
        if (!res.ok) throw new Error(await res.text() || "Ошибка загрузки позиций");
        currentEditingItems = await res.json() || [];
        renderArchiveItemsTable();
    } catch (e) {
        if (tbody) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="7" class="p-8 text-center text-rose-400">
                        ❌ Ошибка загрузки позиций: ${escapeHtml(e.message)}
                    </td>
                </tr>`;
        }
    }
};

function renderArchiveItemsTable() {
    const tbody = document.getElementById('archive-items-tbody');
    if (!tbody) return;
    tbody.innerHTML = "";

    if (!currentEditingItems || currentEditingItems.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="7" class="p-8 text-center text-slate-500">
                    Позиций не найдено
                </td>
            </tr>`;
        updateArchiveEditTotalSum();
        return;
    }

    currentEditingItems.forEach((it, idx) => {
        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827] transition-colors border-b border-slate-800/40 align-middle";

        const q = parseFloat(it.quantity) || 0;
        const m = parseFloat(it.multiplier) || 1.0;
        const finalQ = q * m;
        const totalSum = parseFloat(it.total_sum) || 0;
        const pricePerUnit = (finalQ > 0) ? (totalSum / finalQ) : (parseFloat(it.price_per_base_unit) || 0);

        tr.innerHTML = `
            <td class="px-3 py-3 text-center text-slate-500 font-semibold">${idx + 1}</td>
            <td class="px-3 py-3">
                <div class="font-bold text-white text-xs leading-snug">${escapeHtml(it.product_name_in_invoice)}</div>
                ${it.brand ? `<span class="text-[9px] text-slate-400">Бренд: ${escapeHtml(it.brand)}</span>` : ''}
            </td>
            <td class="px-3 py-3 text-center font-semibold text-slate-300">${q.toFixed(3)}</td>
            <td class="px-3 py-3 text-center text-slate-400 text-[11px]">${escapeHtml(it.unit || 'кг/шт')}</td>
            <td class="px-3 py-3 text-center">
                <input type="text" inputmode="decimal" class="archive-item-final-qty w-20 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 text-center font-bold text-emerald-400"
                    value="${finalQ.toFixed(3)}" oninput="onArchiveItemFinalQtyChange(${idx}, this)">
            </td>
            <td class="px-3 py-3 text-center">
                <input type="text" inputmode="decimal" class="archive-item-total-sum w-24 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 text-center font-bold text-white"
                    value="${totalSum.toFixed(2)}" oninput="onArchiveItemSumChange(${idx}, this)">
            </td>
            <td class="px-3 py-3 text-center font-bold text-brand-400 text-xs">
                <span id="archive-item-price-${idx}">${pricePerUnit.toFixed(2)}</span> ₽
            </td>
        `;
        tbody.appendChild(tr);
    });

    updateArchiveEditTotalSum();
}

window.onArchiveItemFinalQtyChange = function(idx, inputEl) {
    inputEl.value = inputEl.value.replace(/[^0-9.,]/g, '');
    const it = currentEditingItems[idx];
    if (!it) return;

    const finalQ = parseFloat(inputEl.value.replace(',', '.')) || 0;
    const q = parseFloat(it.quantity) || 0;
    it.multiplier = q > 0 ? (finalQ / q) : 1.0;

    const totalSum = parseFloat(it.total_sum) || 0;
    it.price_per_base_unit = finalQ > 0 ? (totalSum / finalQ) : 0;

    const priceSpan = document.getElementById(`archive-item-price-${idx}`);
    if (priceSpan) priceSpan.innerText = it.price_per_base_unit.toFixed(2);
};

window.onArchiveItemSumChange = function(idx, inputEl) {
    inputEl.value = inputEl.value.replace(/[^0-9.,]/g, '');
    const it = currentEditingItems[idx];
    if (!it) return;

    const totalSum = parseFloat(inputEl.value.replace(',', '.')) || 0;
    it.total_sum = totalSum;

    const q = parseFloat(it.quantity) || 0;
    const m = parseFloat(it.multiplier) || 1.0;
    const finalQ = q * m;
    it.price_per_base_unit = finalQ > 0 ? (totalSum / finalQ) : 0;

    const priceSpan = document.getElementById(`archive-item-price-${idx}`);
    if (priceSpan) priceSpan.innerText = it.price_per_base_unit.toFixed(2);

    updateArchiveEditTotalSum();
};

function updateArchiveEditTotalSum() {
    let total = 0;
    if (currentEditingItems) {
        currentEditingItems.forEach(it => {
            total += parseFloat(it.total_sum) || 0;
        });
    }
    const totalEl = document.getElementById('archive-edit-total-sum');
    if (totalEl) totalEl.innerText = total.toFixed(2) + " ₽";
}

window.saveArchiveInvoiceChanges = async function() {
    const companyId = els.company ? els.company.value : "";
    if (!companyId || !currentEditingInvoice || !currentEditingItems) return;

    const btn = document.getElementById('btn-save-archive-items');
    if (btn) {
        btn.disabled = true;
        btn.innerText = "💾 Сохранение...";
    }

    try {
        const payload = {
            company_id: parseInt(companyId),
            invoice_number: currentEditingInvoice.invoice_number,
            items: currentEditingItems.map(it => ({
                id: it.id,
                quantity: parseFloat(it.quantity) || 0,
                multiplier: parseFloat(it.multiplier) || 1.0,
                total_sum: parseFloat(it.total_sum) || 0,
                price_per_base_unit: parseFloat(it.price_per_base_unit) || 0
            }))
        };

        const res = await fetch('api/history/invoice-items', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': getAuthToken()
            },
            body: JSON.stringify(payload)
        });

        if (!res.ok) {
            const errTxt = await res.text();
            throw new Error(errTxt || "Ошибка сохранения изменений");
        }

        alert("✅ Изменения в накладной успешно сохранены в базе данных!");
        backToArchiveInvoicesList();
        loadArchiveInvoices();
    } catch (e) {
        alert("❌ Ошибка сохранения: " + e.message);
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.innerText = "💾 Сохранить изменения";
        }
    }
};
