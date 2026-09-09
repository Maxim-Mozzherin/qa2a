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
        let sWithNds = q * p;
        let sumWithoutNds = sWithNds / (1 + nds/100);
        totalWithNds += sWithNds;
        totalWithoutNds += sumWithoutNds;
    });
    const elWithNds = document.getElementById('footer-total-with-nds');
    const elWithoutNds = document.getElementById('footer-total-without-nds');
    if (elWithNds) elWithNds.innerText = totalWithNds.toFixed(2) + " ₽";
    if (elWithoutNds) elWithoutNds.innerText = totalWithoutNds.toFixed(2) + " ₽";
}
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
function updateFooterTotals() {
    if (!currentDocData || !currentDocData.items) return;
    let totalWithNds = 0;
    let totalWithoutNds = 0;
    currentDocData.items.forEach(item => {
        let q = parseFloat(item.quantity) || 0;
        let p = parseFloat(item.price) || 0;
        let nds = parseFloat(item.nds_percent) || 0;
        let sWithNds = q * p;
        let sumWithoutNds = sWithNds / (1 + nds/100);
        totalWithNds += sWithNds;
        totalWithoutNds += sumWithoutNds;
    });
    const elWithNds = document.getElementById('footer-total-with-nds');
    const elWithoutNds = document.getElementById('footer-total-without-nds');
    if (elWithNds) elWithNds.innerText = totalWithNds.toFixed(2) + " ₽";
    if (elWithoutNds) elWithoutNds.innerText = totalWithoutNds.toFixed(2) + " ₽";
}
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
        els.resConsignee.innerText = data.consignee || "Грузополучатель не распознан";
        els.resConsignee.title = data.consignee || "";
    }
    if (els.resShipper) {
        els.resShipper.innerText = data.shipper || "Грузоотправитель не распознан";
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
                <input type="text" class="w-full bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 font-bold text-slate-100 tracking-tight text-xs pb-1 transition-all" value="${escapeHtml(item.name)}" oninput="currentDocData.items[${idx}].name = this.value;">
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
                <div class="text-[10px] text-slate-500 mt-1"><span class="ai-sum-display">${finalSumWithNds.toFixed(2)}</span> ₽ (всего)</div>
                <div class="text-[9px] text-brand-400 font-semibold mt-1 bg-brand-500/10 rounded px-1.5 py-0.5 inline-block">НДС ${item.nds_percent}%</div>
            </td>
            <td class="px-2 py-3 align-middle">
                <input type="text" list="iiko-catalog-list" class="iiko-search w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" 
                    value="${escapeHtml(prefillName)}" placeholder="Начните вводить или вставьте UUID...">
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <input type="text" inputmode="decimal" oninput="this.value = this.value.replace(/[^0-9.,]/g, '');" class="iiko-mult w-12 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 text-center ${multClass} font-normal" 
                    value="${initMult}">
                ${multBadge}
            </td>
            <td class="px-2 py-3 text-center font-semibold text-emerald-400 text-xs final-qty align-middle">
                ${initFinalQty.toFixed(3)}
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <button class="bg-rose-500/10 text-rose-400 border border-rose-500/20 hover:bg-rose-500/20 transition-all-300 px-2.5 py-1.5 rounded-lg text-[10px] font-bold" onclick="deleteInvoiceItem(${idx})">
                    ✕
                </button>
            </td>
        `;

        const multInput = tr.querySelector('.iiko-mult');
        const finalCell = tr.querySelector('.final-qty');

        const aiQtyInput = tr.querySelector('.ai-qty-input');
        const aiPriceInput = tr.querySelector('.ai-price-input');
        const sumDisplay = tr.querySelector('.ai-sum-display');
        
        function updateRowMath() {
            let m = parseFloat(multInput.value.replace(',', '.')) || 0;
            let q = parseFloat(aiQtyInput.value.replace(',', '.')) || 0;
            let p = parseFloat(aiPriceInput.value.replace(',', '.')) || 0;
            
            currentDocData.items[idx].quantity = q;
            currentDocData.items[idx].price = p;
            currentDocData.items[idx].multiplier = m;
            
            let sWithNds = q * p;
            currentDocData.items[idx].sum = sWithNds;
            
            let nds = parseFloat(currentDocData.items[idx].nds_percent) || 0;
            currentDocData.items[idx].sum_without_nds = sWithNds / (1 + nds/100);
            
            finalCell.innerText = (q * m).toFixed(3);
            sumDisplay.innerText = sWithNds.toFixed(2);
            updateFooterTotals();
        }

        multInput.addEventListener('input', updateRowMath);
        if(aiQtyInput) aiQtyInput.addEventListener('input', updateRowMath);
        if(aiPriceInput) aiPriceInput.addEventListener('input', updateRowMath);

        els.tbody.appendChild(tr);
    });

    if (data.items.length > 0) {
        const totalTr = document.createElement('tr');
        totalTr.className = "bg-[#090d16] font-semibold border-t border-slate-800 text-slate-300 text-xs align-middle";
        totalTr.innerHTML = `
            <td class="p-4 text-center text-slate-500">∑</td>
            <td class="p-4 text-left uppercase text-[10px] font-semibold tracking-wider text-slate-500" colspan="2">Итого накладная:</td>
            <td class="p-4 text-left align-middle border-r border-slate-800/40" colspan="5">
                <span class="text-slate-500 font-normal">Без НДС:</span> 
                <span id="footer-total-without-nds" class="text-slate-200 font-semibold mr-6">${totalWithoutNds.toFixed(2)} ₽</span>
                <span class="text-slate-500 font-normal">С НДС:</span> 
                <span id="footer-total-with-nds" class="text-brand-400 font-semibold">${totalWithNds.toFixed(2)} ₽</span>
            </td>
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