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
    if (currentCustomPrompt) {
        formData.append('prompt', currentCustomPrompt);
    }
    const selectedModel = (els.modelSelect && els.modelSelect.value) || localStorage.getItem('iiko_selected_ai_model') || 'gemini-3-flash';
    formData.append('model', selectedModel);

    els.btnParse.disabled = true;
    els.loaderParse.classList.remove('hidden');
    els.resSection.classList.add('hidden');

    try {
        const res = await fetch('api/parse', {
            method: 'POST',
            headers: { 'Authorization': 'Bearer ' + getAuthToken() },
            body: formData
        });

        if (!res.ok) throw new Error(await res.text());

        currentDocData = await res.json();
        if (currentDocData && currentDocData.used_model) {
            window.lastUsedModel = currentDocData.used_model;
            if (els.usedModelBadge) {
                els.usedModelBadge.textContent = '🤖 Модель: ' + currentDocData.used_model;
                els.usedModelBadge.classList.remove('hidden');
            }
        }
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
    const selectedModel = (els.modelSelect && els.modelSelect.value) || localStorage.getItem('iiko_selected_ai_model') || 'gemini-3-flash';
    formData.append('model', selectedModel);

    els.btnAddPage.disabled = true;
    els.loaderAddPage.classList.remove('hidden');

    try {
        const res = await fetch('api/parse', {
            method: 'POST',
            headers: { 'Authorization': 'Bearer ' + getAuthToken() },
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

    let totalPositions = currentDocData.items.length;
    let totalQty = 0;
    let totalWithNds = 0;
    let totalWithoutNds = 0;

    currentDocData.items.forEach(item => {
        let q = parseFloat(item.quantity) || 0;
        let p = parseFloat(item.price) || 0;
        let nds = parseFloat(item.nds_percent) || 0;
        let sWithNds = (typeof item.sum === 'number' && !isNaN(item.sum)) ? item.sum : (q * p);
        let sWithoutNds = (typeof item.sum_without_nds === 'number' && item.sum_without_nds > 0) 
            ? item.sum_without_nds 
            : (sWithNds / (1 + nds / 100));

        totalQty += q;
        totalWithNds += sWithNds;
        totalWithoutNds += sWithoutNds;
    });

    const elCount = document.getElementById('footer-total-positions');
    const elQty = document.getElementById('footer-total-qty');
    const elWithNds = document.getElementById('footer-total-with-nds');
    const elWithoutNds = document.getElementById('footer-total-without-nds');

    if (elCount) elCount.innerText = `${totalPositions} поз.`;
    if (elQty) elQty.innerText = `${totalQty.toFixed(3)}`;
    if (elWithNds) elWithNds.innerText = `${totalWithNds.toFixed(2)} ₽`;
    if (elWithoutNds) elWithoutNds.innerText = `${totalWithoutNds.toFixed(2)} ₽`;
}

window.onInlineStoreChange = function(val) {
    if (els.store) {
        els.store.value = val;
    }
    const companyId = els.company ? els.company.value : "";
    if (companyId && val) {
        localStorage.setItem(`saved_store_${companyId}`, val);
    }
};
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
        unit: "шт",
        base_unit: "кг/шт",
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

    const companyId = els.company ? els.company.value : "";

    const savedStore = localStorage.getItem(`saved_store_${companyId}`);
    // Prioritize manually saved store if present, fallback to mapped store from consignee
    if (savedStore && els.store && Array.from(els.store.options).some(o => o.value === savedStore)) {
        els.store.value = savedStore;
    } else if (data.mapped_store_uuid && els.store) {
        els.store.value = data.mapped_store_uuid;
    }

    // Sync the inline store selector in the results table header
    const inlineStore = document.getElementById('res-inline-store');
    if (inlineStore && els.store) {
        inlineStore.innerHTML = els.store.innerHTML;
        inlineStore.value = els.store.value;
        inlineStore.disabled = false;
    }

    if (data.mapped_supplier_uuid) {
        const cleanSupUuid = (data.mapped_supplier_uuid || '').toLowerCase().trim();
        const foundSup = iikoSuppliers.find(s => (s.uuid || '').toLowerCase().trim() === cleanSupUuid);
        if (foundSup && els.supplierSearch) {
            els.supplierSearch.value = foundSup.name;
        }
    } else {
        const savedSupplierName = localStorage.getItem(`saved_supplier_name_${companyId}`);
        if (savedSupplierName && els.supplierSearch) els.supplierSearch.value = savedSupplierName;
    }

    const inlineSupplierName = document.getElementById('res-inline-supplier-name');
    if (inlineSupplierName) {
        inlineSupplierName.innerText = (els.supplierSearch && els.supplierSearch.value) ? els.supplierSearch.value : '—';
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
        tr.className = "hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle invoice-item-row";
        tr.dataset.itemIdx = idx;

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
        const ndsBadgeText = ndsPercent > 0 ? `НДС ${ndsPercent}%` : 'Без НДС';

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
            <td class="px-3 py-3 align-middle">
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
                <div class="flex items-center justify-center gap-1.5">
                    <input type="text" inputmode="decimal" class="w-14 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 outline-none focus:border-brand-500 text-center font-extrabold text-slate-200 text-xs transition-all ai-qty-input" value="${aiQty}" title="Количество в накладной">
                    <input type="text" class="w-12 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 outline-none focus:border-brand-500 text-center text-slate-400 text-xs transition-all ai-unit-input" value="${escapeHtml(item.unit || 'кг/шт')}" title="Единица измерения УПД">
                </div>
            </td>
            <td class="px-3 py-3 align-middle">
                <input type="text" list="iiko-catalog-list" class="iiko-search w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" 
                    value="${escapeHtml(prefillName)}" placeholder="Начните вводить или вставьте UUID...">
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <div class="flex items-center justify-center gap-1.5">
                    <input type="text" inputmode="decimal" oninput="this.value = this.value.replace(/[^0-9.,]/g, '');" class="iiko-final-qty w-16 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 text-center ${multClass} font-bold" 
                        value="${initFinalQty.toFixed(3)}" title="Итоговое оприходование в iiko">
                    <input type="text" class="iiko-base-unit-input w-12 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-[11px] outline-none focus:bg-[#111827] focus:border-brand-500 text-center text-slate-400 font-semibold" 
                        value="${escapeHtml(item.base_unit || item.unit || 'кг/шт')}" title="Базовая единица для iiko">
                </div>
                ${multBadge}
            </td>
            <td class="px-3 py-3 border-l border-slate-800/40 align-middle">
                <div class="flex items-center justify-center gap-2 flex-nowrap">
                    <!-- Цена за единицу товара -->
                    <div class="flex items-center gap-1 bg-[#090d16] border border-slate-800 rounded-lg px-2.5 py-1.5 focus-within:border-brand-500 transition-all" title="Цена за единицу (с НДС)">
                        <input type="text" inputmode="decimal" class="w-16 bg-transparent outline-none text-right font-semibold text-white text-xs transition-all ai-price-input" value="${item.price.toFixed(2)}">
                        <span class="text-[10px] text-slate-400 whitespace-nowrap price-unit-label font-medium">₽/${escapeHtml(item.unit || 'ед.')}</span>
                    </div>

                    <span class="text-slate-600 font-light select-none">|</span>

                    <!-- Общая сумма с НДС -->
                    <div class="flex items-center gap-1 bg-[#090d16] border border-slate-800 rounded-lg px-2.5 py-1.5 focus-within:border-brand-500 transition-all" title="Сумма с НДС">
                        <input type="text" inputmode="decimal" class="w-20 bg-transparent outline-none text-right font-bold text-slate-200 text-xs transition-all ai-sum-input" value="${finalSumWithNds.toFixed(2)}">
                        <span class="text-[10px] text-slate-400 font-medium">₽</span>
                    </div>

                    <!-- Ставка НДС -->
                    <span class="text-[9px] text-brand-400 font-bold bg-brand-500/10 border border-brand-500/20 rounded-md px-1.5 py-1 whitespace-nowrap select-none" title="Ставка НДС">${ndsBadgeText}</span>
                </div>
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <button class="bg-rose-500/10 text-rose-400 border border-rose-500/20 hover:bg-rose-500/20 transition-all-300 px-2.5 py-1.5 rounded-lg text-[10px] font-bold" onclick="deleteInvoiceItem(${idx})" title="Удалить позицию">
                    ✕
                </button>
            </td>
        `;

        const finalQtyInput = tr.querySelector('.iiko-final-qty');
        const baseUnitInput = tr.querySelector('.iiko-base-unit-input');
        const aiQtyInput = tr.querySelector('.ai-qty-input');
        const aiPriceInput = tr.querySelector('.ai-price-input');
        const sumInput = tr.querySelector('.ai-sum-input');
        const priceUnitLabel = tr.querySelector('.price-unit-label');

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
            runDataValidation();
        };

        const aiUnitInput = tr.querySelector('.ai-unit-input');
        if (finalQtyInput) finalQtyInput.addEventListener('input', recalcFromFinalQty);
        if (baseUnitInput) {
            baseUnitInput.addEventListener('input', () => {
                if (currentDocData.items[idx]) {
                    currentDocData.items[idx].base_unit = baseUnitInput.value.trim() || 'кг/шт';
                }
            });
        }
        if (aiQtyInput) aiQtyInput.addEventListener('input', recalcFromQtyPrice);
        if (aiUnitInput) {
            aiUnitInput.addEventListener('input', () => {
                const u = aiUnitInput.value.trim() || 'кг/шт';
                if (currentDocData.items[idx]) {
                    currentDocData.items[idx].unit = u;
                }
                if (priceUnitLabel) {
                    priceUnitLabel.innerText = `₽/${u}`;
                }
            });
        }
        if (aiPriceInput) aiPriceInput.addEventListener('input', recalcFromQtyPrice);
        if (sumInput) sumInput.addEventListener('input', recalcFromSum);

        const iikoSearchInput = tr.querySelector('.iiko-search');
        if (iikoSearchInput) {
            iikoSearchInput.addEventListener('input', () => {
                iikoSearchInput.classList.remove('border-red-500/80', 'bg-red-500/10', 'ring-2', 'ring-red-500/20');
            });
        }

        els.tbody.appendChild(tr);
    });

    if (data.items.length > 0) {
        const totalTr = document.createElement('tr');
        totalTr.className = "bg-[#090d16] font-semibold border-t-2 border-slate-700 text-slate-300 text-xs align-middle";
        totalTr.innerHTML = `
            <td class="p-3 text-center text-slate-500 font-bold">∑</td>
            <td class="p-3 font-bold uppercase text-[10px] tracking-wider text-slate-400">
                Контрольные суммы накладной:
            </td>
            <td class="p-3 text-left">
                <span class="text-slate-500 text-[10px] block">Строк:</span>
                <span id="footer-total-positions" class="font-extrabold text-white text-xs">0 поз.</span>
            </td>
            <td class="p-3 text-center">
                <span class="text-slate-500 text-[10px] block">Σ Нетто (объем):</span>
                <span id="footer-total-qty" class="font-extrabold text-emerald-400 text-xs">0.000</span>
            </td>
            <td class="p-3 text-slate-500 text-[10px]" colspan="2">
                Сверьте эти три значения с итогами УПД/накладной
            </td>
            <td class="p-3 text-center border-l border-slate-800/60">
                <div class="flex items-center justify-center gap-3 flex-nowrap">
                    <div class="text-[10px] text-slate-400 whitespace-nowrap">Без НДС: <span id="footer-total-without-nds" class="text-slate-200 font-semibold">0.00 ₽</span></div>
                    <span class="text-slate-600 font-light select-none">|</span>
                    <div class="text-xs font-black text-brand-400 whitespace-nowrap">С НДС: <span id="footer-total-with-nds">0.00 ₽</span></div>
                </div>
            </td>
            <td class="p-3"></td>
        `;
        els.tbody.appendChild(totalTr);
        updateFooterTotals();
    }
    runDataValidation();
}

function runDataValidation() {
    if (!currentDocData || !currentDocData.items || currentDocData.items.length === 0) return;

    let errors = [];
    let calculatedTotal = 0;
    
    // Arrays to track sequence anomalies
    let missingNums = [];
    let duplicateNums = [];
    let prevNum = 0;


    currentDocData.items.forEach((item, idx) => {
        const q = parseFloat(item.quantity) || 0;
        const p = parseFloat(item.price) || 0;
        const s = parseFloat(item.sum) || 0;
        calculatedTotal += s;

        // 1. Math Validation (Row level)
        if (q > 0 && Math.abs((q * p) - s) > 0.05) {
            errors.push(`Строка ${idx + 1} (${item.name || 'Без названия'}): Математика не сходится (Кол-во × Цена ≠ Сумма)`);
            const row = document.querySelector(`tr[data-item-idx="${idx}"]`);
            if (row) row.classList.add('bg-amber-500/10');
        } else {
            const row = document.querySelector(`tr[data-item-idx="${idx}"]`);
            if (row) row.classList.remove('bg-amber-500/10');
        }

        // 2. Sequence Gap & Duplicate Validation
        const currentNum = parseInt(item.num, 10);
        if (currentNum > 0) {
            if (prevNum > 0) {
                if (currentNum === prevNum) {
                    // LLM hallucinated and parsed the same row twice
                    duplicateNums.push(currentNum);
                } else if (currentNum > prevNum + 1) {
                    // Gap detected! LLM skipped rows
                    for (let m = prevNum + 1; m < currentNum; m++) {
                        missingNums.push(m);
                    }
                } else if (currentNum < prevNum) {
                     // Minor failsafe if the AI output order gets completely jumbled, though rare
                     errors.push(`Нарушен порядок строк: №${currentNum} идет после №${prevNum}`);
                }
            }
            prevNum = currentNum;
        }

        // 3. Hallucination Anchor Validation (original_prefix)
        if (item.original_prefix && item.name) {
            let prefixRaw = item.original_prefix.toLowerCase().replace(/[^a-zа-я0-9]/g, '');
            let nameRaw = item.name.toLowerCase().replace(/[^a-zа-я0-9]/g, '');
            
            if (prefixRaw.length > 2 && nameRaw && !nameRaw.includes(prefixRaw)) {
                errors.push(`Строка ${idx + 1}: Название "${item.name}" не содержит якорь "${item.original_prefix}". Подозрение на выдумку ИИ.`);
                const row = document.querySelector(`tr[data-item-idx="${idx}"]`);
                if (row) {
                    row.classList.add('bg-orange-500/10');
                    let nameInput = row.querySelector('input[data-field="name"]');
                    if (nameInput && !nameInput.parentNode.querySelector('.hallucination-warn')) {
                        const warnIcon = document.createElement('span');
                        warnIcon.innerHTML = '⚠️';
                        warnIcon.className = 'hallucination-warn cursor-help ml-2 text-xl transition-all hover:scale-110';
                        warnIcon.title = 'AI сомневается. Исходное слово на скане: "' + item.original_prefix + '"';
                        nameInput.parentNode.appendChild(warnIcon);
                    }
                }
            }
        }
    });

    // Report Sequence Anomalies
    if (missingNums.length > 0) {
        errors.push(`Нейросеть пропустила строки УПД: порядковые номера [${missingNums.join(', ')}] не найдены в таблице.`);
    }
    if (duplicateNums.length > 0) {
        errors.push(`Сбой нумерации УПД: строка № [${duplicateNums.join(', ')}] распознана несколько раз.`);
    }


    // 3. Total Sum Validation (Tolerance 1.00 Ruble)
    const printedTotal = parseFloat(currentDocData.doc_printed_total_sum) || 0;
    if (printedTotal > 0 && Math.abs(calculatedTotal - printedTotal) > 1.0) {
        errors.push(`Итоговая сумма не совпадает: Вычислено ${calculatedTotal.toFixed(2)} ₽, в документе (AI) распознано ${printedTotal.toFixed(2)} ₽`);
    }

    // DOM Updates
    const banner = document.getElementById('validation-warning-banner');
    const errList = document.getElementById('validation-errors-list');
    const forceCheckbox = document.getElementById('force-submit-checkbox');
    const btnImport = document.getElementById('btn-import');

    if (errors.length > 0) {
        if (errList) errList.innerHTML = errors.map(e => `<li>${escapeHtml(e)}</li>`).join('');
        if (banner) banner.classList.remove('hidden');
        
        if (btnImport && forceCheckbox && !forceCheckbox.checked) {
            btnImport.disabled = true;
        }
    } else {
        if (banner) banner.classList.add('hidden');
        if (forceCheckbox) forceCheckbox.checked = false;
        if (btnImport) btnImport.disabled = false;
    }
}

document.addEventListener('DOMContentLoaded', () => {
    const forceCb = document.getElementById('force-submit-checkbox');
    const btnImport = document.getElementById('btn-import');
    if (forceCb && btnImport) {
        forceCb.addEventListener('change', (e) => {
            btnImport.disabled = !e.target.checked;
        });
    }
});
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
let archivePollInterval = null;

window.openInvoiceArchive = function() {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) {
        alert("⚠️ Пожалуйста, сначала выберите заведение в шапке страницы!");
        return;
    }
    const modal = document.getElementById('invoice-archive-modal');
    if (modal) {
        modal.classList.remove('hidden');
        backToArchiveInvoicesList(); // This sets up the view
        loadArchiveInvoices();
        
        // Start polling every 5 seconds
        if (archivePollInterval) clearInterval(archivePollInterval);
        archivePollInterval = setInterval(() => {
            // Only poll if the list view is active (not editing an invoice)
            const invoicesView = document.getElementById('archive-invoices-view');
            if (invoicesView && !invoicesView.classList.contains('hidden')) {
                loadArchiveInvoices(true); // pass true to indicate background polling
            }
        }, 5000);
    }
};

window.closeInvoiceArchive = function() {
    const modal = document.getElementById('invoice-archive-modal');
    if (modal) modal.classList.add('hidden');
    if (archivePollInterval) {
        clearInterval(archivePollInterval);
        archivePollInterval = null;
    }
};

window.backToArchiveInvoicesList = function() {
    const invoicesView = document.getElementById('archive-invoices-view');
    const itemsView = document.getElementById('archive-items-view');
    if (invoicesView) invoicesView.classList.remove('hidden');
    if (itemsView) itemsView.classList.add('hidden');
    currentEditingInvoice = null;
    currentEditingItems = [];
};

window.loadArchiveInvoices = async function(isBackground = false) {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) return;

    const tbody = document.getElementById('archive-invoices-tbody');
    if (!tbody) return;

    if (!isBackground) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" class="p-8 text-center text-slate-400">
                    <div class="inline-block w-5 h-5 border-2 border-brand-500 border-t-transparent rounded-full animate-spin mr-2 align-middle"></div>
                    Загрузка архива накладных...
                </td>
            </tr>`;
    }

    try {
        const res = await fetch(`api/history/invoices?company_id=${companyId}`, {
            headers: {
                'Authorization': getAuthToken()
            }
        });
        if (!res.ok) throw new Error(await res.text() || "Ошибка загрузки списка");
        archiveInvoices = await res.json() || [];
        const searchInput = document.getElementById('archive-search-input');
        if (searchInput && searchInput.value.trim()) {
            filterArchiveInvoices();
        } else {
            renderArchiveInvoicesTable(archiveInvoices); // This will silently replace HTML
        }
    } catch (e) {
        if (!isBackground) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="p-8 text-center text-rose-400">
                        ❌ Ошибка загрузки: ${escapeHtml(e.message)}
                    </td>
                </tr>`;
        }
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
            <td class="px-4 py-3.5 text-slate-300 whitespace-normal break-words min-w-[250px] max-w-sm leading-snug" title="${escapeHtml(inv.supplier_name)}">${escapeHtml(inv.supplier_name || '—')}</td>
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
    if (vendorEl) vendorEl.value = supplierName || '';

    const tbody = document.getElementById('archive-items-tbody');
    if (tbody) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" class="p-8 text-center text-slate-400">
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
                    <td colspan="6" class="p-8 text-center text-rose-400">
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

    const priceHeader = document.querySelector('#archive-items-view thead tr th:last-child');
    if (priceHeader) priceHeader.innerText = "Цена (за ед. iiko)";

    if (!currentEditingItems || currentEditingItems.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="6" class="p-8 text-center text-slate-500">
                    Позиций не найдено
                </td>
            </tr>`;
        updateArchiveEditTotalSum();
        return;
    }

    currentEditingItems.forEach((it, idx) => {
        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827] transition-colors border-b border-slate-800/40 align-middle";

        const rawQ = parseFloat(it.quantity) || 0;
        const rawM = parseFloat(it.multiplier) || 1.0;
        const finalQ = it.final_qty !== undefined ? (parseFloat(it.final_qty) || 0) : (rawQ * rawM);
        const totalSum = it.total_sum !== undefined ? (parseFloat(it.total_sum) || 0) : 0;
        const pricePerUnit = (it.price_per_base_unit !== undefined && parseFloat(it.price_per_base_unit) > 0)
            ? parseFloat(it.price_per_base_unit)
            : (finalQ > 0 ? (totalSum / finalQ) : 0);

        it.final_qty = finalQ;
        it.unit = it.unit || 'кг/шт';
        it.total_sum = totalSum;
        it.price_per_base_unit = isFinite(pricePerUnit) ? pricePerUnit : 0;

        tr.innerHTML = `
            <td class="px-3 py-3 text-center text-slate-500 font-semibold">${idx + 1}</td>
            <td class="px-3 py-3">
                <div class="font-bold text-white text-xs leading-snug">${escapeHtml(it.product_name_in_invoice)}</div>
                ${it.brand ? `<span class="text-[9px] text-slate-400">Бренд: ${escapeHtml(it.brand)}</span>` : ''}
            </td>
            <td class="px-3 py-3 text-center">
                <input type="text" inputmode="decimal" id="archive-qty-input-${it.id}" class="w-16 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:border-brand-500 text-center font-bold text-emerald-400 archive-item-qty" value="${finalQ.toFixed(3)}" oninput="onArchiveItemFinalQtyChange(${idx}, this)">
            </td>
            <td class="px-3 py-3 text-center">
                <input type="text" id="archive-unit-input-${it.id}" class="w-12 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:border-brand-500 text-center text-slate-300 archive-item-unit" value="${escapeHtml(it.unit || 'кг/шт')}">
            </td>
            <td class="px-3 py-3 text-center">
                <input type="text" inputmode="decimal" id="archive-sum-input-${it.id}" class="w-20 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:border-brand-500 text-center font-bold text-white archive-item-sum" value="${totalSum.toFixed(2)}" oninput="onArchiveItemSumChange(${idx}, this)">
            </td>
            <td class="px-3 py-3 text-center">
                <input type="text" inputmode="decimal" id="archive-price-input-${it.id}" class="w-20 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:border-brand-500 text-center font-bold text-brand-400 archive-item-price" value="${pricePerUnit.toFixed(2)}" oninput="onArchiveItemPriceChange(${idx}, this)">
            </td>
        `;

        const unitInput = tr.querySelector(`#archive-unit-input-${it.id}`);
        if (unitInput) {
            unitInput.addEventListener('input', () => {
                it.unit = unitInput.value.trim() || 'кг/шт';
            });
        }

        tbody.appendChild(tr);
    });

    updateArchiveEditTotalSum();
}

window.onArchiveItemFinalQtyChange = function(idx, inputEl) {
    inputEl.value = inputEl.value.replace(/[^0-9.,]/g, '');
    const it = currentEditingItems[idx];
    if (!it) return;

    const finalQ = parseFloat(inputEl.value.replace(',', '.')) || 0;
    it.final_qty = finalQ;

    const sumVal = parseFloat(it.total_sum) || 0;
    const price = finalQ > 0 ? (sumVal / finalQ) : 0;
    it.price_per_base_unit = isFinite(price) ? price : 0;

    const priceInput = document.getElementById(`archive-price-input-${it.id}`) || inputEl.closest('tr')?.querySelector('.archive-item-price');
    if (priceInput) {
        priceInput.value = it.price_per_base_unit > 0 ? it.price_per_base_unit.toFixed(2) : '0.00';
    }
};

window.onArchiveItemSumChange = function(idx, inputEl) {
    inputEl.value = inputEl.value.replace(/[^0-9.,]/g, '');
    const it = currentEditingItems[idx];
    if (!it) return;

    const totalSum = parseFloat(inputEl.value.replace(',', '.')) || 0;
    it.total_sum = totalSum;

    const finalQ = parseFloat(it.final_qty) || parseFloat(it.quantity) || 0;
    const price = finalQ > 0 ? (totalSum / finalQ) : 0;
    it.price_per_base_unit = isFinite(price) ? price : 0;

    const priceInput = document.getElementById(`archive-price-input-${it.id}`) || inputEl.closest('tr')?.querySelector('.archive-item-price');
    if (priceInput) {
        priceInput.value = it.price_per_base_unit > 0 ? it.price_per_base_unit.toFixed(2) : '0.00';
    }

    updateArchiveEditTotalSum();
};

window.onArchiveItemPriceChange = function(idx, inputEl) {
    inputEl.value = inputEl.value.replace(/[^0-9.,]/g, '');
    const it = currentEditingItems[idx];
    if (!it) return;

    const price = parseFloat(inputEl.value.replace(',', '.')) || 0;
    it.price_per_base_unit = price;

    const finalQ = parseFloat(it.final_qty) || parseFloat(it.quantity) || 0;
    const newSum = price * finalQ;
    it.total_sum = isFinite(newSum) ? newSum : 0;

    const sumInput = document.getElementById(`archive-sum-input-${it.id}`) || inputEl.closest('tr')?.querySelector('.archive-item-sum');
    if (sumInput) {
        sumInput.value = it.total_sum > 0 ? it.total_sum.toFixed(2) : '0.00';
    }

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
        const vendorEl = document.getElementById('archive-edit-vendor');
        const newVendorName = vendorEl ? vendorEl.value.trim() : (currentEditingInvoice.supplier_name || '');

        const payload = {
            company_id: parseInt(companyId),
            invoice_number: currentEditingInvoice.invoice_number,
            supplier_name: newVendorName,
            items: currentEditingItems.map(it => {
                // Determine the multiplier. If the user edited the final iiko quantity, 
                // we force multiplier to 1.0 and store the final amount directly in quantity,
                // because we are bypassing the raw document amount.
                const finalQ = parseFloat(it.final_qty) || parseFloat(it.quantity) || 0;
                return {
                    id: it.id,
                    final_qty: finalQ,
                    unit: document.getElementById(`archive-unit-input-${it.id}`)?.value.trim() || it.unit || "кг/шт",
                    total_sum: parseFloat(it.total_sum) || 0,
                    price_per_base_unit: parseFloat(it.price_per_base_unit) || 0
                };
            })
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

        currentEditingInvoice.supplier_name = newVendorName;

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
