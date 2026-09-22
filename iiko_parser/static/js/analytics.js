window.loadAnalytics = async function() {
    const companyId = els.company ? els.company.value : "";
    if (!companyId) return;

    const days = document.getElementById('analytics-period')?.value || "30";

    const toxicTbody = document.getElementById('toxic-writeoffs-tbody');
    const priceTbody = document.getElementById('analytics-tbody');

    if (toxicTbody) {
        toxicTbody.innerHTML = `<tr><td colspan="6" class="p-8 text-center text-slate-500"><div class="inline-block w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin mr-2 align-middle"></div>Загрузка анализа списаний...</td></tr>`;
    }
    if (priceTbody) {
        priceTbody.innerHTML = `<tr><td colspan="4" class="p-8 text-center text-slate-500"><div class="inline-block w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin mr-2 align-middle"></div>Загрузка мониторинга цен...</td></tr>`;
    }

    try {
        const [priceRes, toxicRes] = await Promise.all([
            fetch(`api/analytics?company_id=${companyId}&days=${days}`, {
                headers: { 'Authorization': getAuthToken() }
            }),
            fetch(`api/toxic-writeoffs?company_id=${companyId}&days=${days}`, {
                headers: { 'Authorization': getAuthToken() }
            })
        ]);

        if (priceRes.ok) {
            const priceData = await priceRes.json() || [];
            renderPriceAnalyticsTable(priceData);
        } else {
            const errTxt = await priceRes.text();
            if (priceTbody) priceTbody.innerHTML = `<tr><td colspan="4" class="p-8 text-center text-rose-400">❌ Ошибка загрузки цен: ${escapeHtml(errTxt)}</td></tr>`;
        }

        if (toxicRes.ok) {
            const toxicData = await toxicRes.json() || [];
            renderToxicWriteoffsTable(toxicData);
        } else {
            const errTxt = await toxicRes.text();
            if (toxicTbody) toxicTbody.innerHTML = `<tr><td colspan="6" class="p-8 text-center text-rose-400">❌ Ошибка загрузки списаний: ${escapeHtml(errTxt)}</td></tr>`;
        }
    } catch (e) {
        console.error("Ошибка загрузки аналитики:", e);
        if (toxicTbody) toxicTbody.innerHTML = `<tr><td colspan="6" class="p-8 text-center text-rose-400">❌ Сбой сети: ${escapeHtml(e.message)}</td></tr>`;
        if (priceTbody) priceTbody.innerHTML = `<tr><td colspan="4" class="p-8 text-center text-rose-400">❌ Сбой сети: ${escapeHtml(e.message)}</td></tr>`;
    }
};

function renderToxicWriteoffsTable(data) {
    const toxicTbody = document.getElementById('toxic-writeoffs-tbody');
    const banner = document.getElementById('toxic-summary-banner');
    const totalLossEl = document.getElementById('toxic-total-loss');

    if (!toxicTbody) return;

    if (!data || data.length === 0) {
        toxicTbody.innerHTML = `<tr><td colspan="6" class="p-8 text-center text-slate-500">За выбранный период ручных списаний не зафиксировано 🎉</td></tr>`;
        if (totalLossEl) totalLossEl.textContent = '0.00 ₽';
        if (banner) banner.classList.remove('hidden');
        return;
    }

    const totalLoss = data.reduce((acc, row) => acc + (row.total_loss_rub || 0), 0);
    if (totalLossEl) {
        totalLossEl.textContent = totalLoss.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + ' ₽';
    }
    if (banner) {
        banner.classList.remove('hidden');
    }

    toxicTbody.innerHTML = data.map((row, idx) => {
        let badgeHtml = '';
        const wasteRatio = row.waste_ratio_pct || 0;
        if (wasteRatio > 15) {
            badgeHtml = `<span class="px-2 py-0.5 rounded-full text-[10px] font-bold bg-rose-950/60 text-rose-300 border border-rose-800/50">${wasteRatio.toFixed(1)}%</span>`;
        } else if (wasteRatio > 5) {
            badgeHtml = `<span class="px-2 py-0.5 rounded-full text-[10px] font-bold bg-amber-950/60 text-amber-300 border border-amber-800/50">${wasteRatio.toFixed(1)}%</span>`;
        } else {
            badgeHtml = `<span class="px-2 py-0.5 rounded-full text-[10px] font-semibold text-slate-400">${wasteRatio > 0 ? wasteRatio.toFixed(1) + '%' : '—'}</span>`;
        }

        const unitStr = (row.unit && row.unit.trim() !== '') ? escapeHtml(row.unit) : 'кг/шт';

        return `
            <tr class="cursor-pointer hover:bg-slate-800/40 select-none transition-colors border-b border-slate-800/40 align-middle" onclick="toggleToxicAccordion(${idx})">
                <td class="p-3.5 text-center font-bold text-slate-500">
                    <span id="toxic-chevron-${idx}" class="transition-transform inline-block text-[10px]">▶</span> ${idx + 1}
                </td>
                <td class="p-3.5">
                    <div class="text-white font-bold">${escapeHtml(row.position_name || '')}</div>
                </td>
                <td class="p-3.5 text-center font-semibold text-slate-300">
                    ${(row.total_written_off || 0).toFixed(2)} <span class="text-xs text-slate-400 font-normal">${unitStr}</span>
                </td>
                <td class="p-3.5 text-right font-semibold text-slate-300">
                    ${(row.avg_purchase_price || 0).toFixed(2)} ₽ / <span class="text-xs text-slate-400 font-normal">${unitStr}</span>
                </td>
                <td class="p-3.5 text-right">
                    <span class="text-rose-400 font-black text-xs">${(row.total_loss_rub || 0).toFixed(2)} ₽</span>
                </td>
                <td class="p-3.5 text-center">
                    ${badgeHtml}
                </td>
            </tr>
            <tr id="toxic-accordion-${idx}" class="hidden bg-[#090d16]/90 border-b border-slate-800/60 transition-all">
                <td colspan="6" class="p-4 pl-12">
                    <div class="space-y-2">
                        <div class="text-[10px] font-bold text-slate-400 uppercase tracking-wider flex items-center gap-2">
                            <span>📑 Распределение списаний по статьям расхода:</span>
                        </div>
                        <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2.5 pt-1">
                            <!-- Accounts breakdown pills -->
                            ${renderBreakdownPills(row.breakdown, unitStr)}
                        </div>
                    </div>
                </td>
            </tr>
        `;
    }).join('');
}

function renderBreakdownPills(breakdown, unit) {
    if (!breakdown || breakdown.length === 0) {
        return '<span class="text-slate-500 text-xs italic">Нет детализации по статьям</span>';
    }
    const safeUnit = (unit && unit.trim() !== '') ? escapeHtml(unit) : 'кг/шт';
    return breakdown.map(b => {
        let icon = '📦';
        const nameLower = (b.account_name || '').toLowerCase();
        if (nameLower.includes('стафф') || nameLower.includes('питан')) icon = '🥪';
        else if (nameLower.includes('порч') || nameLower.includes('брак') || nameLower.includes('бой')) icon = '🗑️';
        else if (nameLower.includes('проработ')) icon = '🍳';
        else if (nameLower.includes('представительск')) icon = '☕';

        return `
            <div class="bg-[#111827] border border-slate-800 rounded-xl p-2.5 flex items-center justify-between gap-3 shadow-inner">
                <div class="flex items-center gap-2 min-w-0">
                    <span class="text-sm shrink-0">${icon}</span>
                    <span class="text-xs text-slate-300 font-semibold truncate" title="${escapeHtml(b.account_name)}">${escapeHtml(b.account_name)}</span>
                </div>
                <div class="text-right shrink-0">
                    <div class="text-xs font-black text-white">${(b.quantity || 0).toFixed(2)} ${safeUnit}</div>
                    <div class="text-[10px] font-bold text-brand-400">${(b.share_pct || 0).toFixed(1)}%</div>
                </div>
            </div>
        `;
    }).join('');
}

window.toggleToxicAccordion = function(idx) {
    const row = document.getElementById(`toxic-accordion-${idx}`);
    const chevron = document.getElementById(`toxic-chevron-${idx}`);
    if (!row) return;
    if (row.classList.contains('hidden')) {
        row.classList.remove('hidden');
        if (chevron) chevron.style.transform = 'rotate(90deg)';
    } else {
        row.classList.add('hidden');
        if (chevron) chevron.style.transform = 'rotate(0deg)';
    }
};

function processPriceData(records) {
    if (!records || records.length === 0) return [];
    if (records[0] && (records[0].last_price !== undefined || records[0].latestPrice !== undefined)) {
        return records;
    }

    const grouped = {};
    records.forEach(r => {
        const key = (r.iiko_product_uuid && r.iiko_product_uuid.trim() !== "") ? r.iiko_product_uuid : (r.iiko_product_name || r.product_name);
        const displayName = r.iiko_product_name || r.product_name;
        if (!grouped[key]) {
            grouped[key] = {
                name: displayName,
                invoiceName: r.product_name,
                uuid: r.iiko_product_uuid || "",
                unit: r.unit || 'ед.',
                prices: [],
                latestDate: '1970-01-01',
                latestPrice: 0,
                last_price: 0,
                median_price: 0,
                medianPrice: 0,
                price_diff: 0,
                latestSupplier: ''
            };
        }

        const price = parseFloat(r.price_per_base_unit || 0);
        grouped[key].prices.push(price);
        if (r.unit) {
            grouped[key].unit = r.unit;
        }

        if (r.invoice_date >= grouped[key].latestDate) {
            grouped[key].latestDate = r.invoice_date;
            grouped[key].latestPrice = price;
            grouped[key].last_price = price;
            grouped[key].latestSupplier = r.supplier_name;
        }
    });

    const items = Object.values(grouped).map(g => {
        g.prices.sort((a, b) => a - b);
        const mid = Math.floor(g.prices.length / 2);
        let median = 0;
        if (g.prices.length > 0) {
            if (g.prices.length % 2 !== 0) {
                median = g.prices[mid];
            } else {
                median = (g.prices[mid - 1] + g.prices[mid]) / 2.0;
            }
        }
        g.medianPrice = median;
        g.median_price = median;
        g.last_price = g.last_price || g.latestPrice || 0;
        g.price_diff = median > 0 ? ((g.last_price - median) / median) * 100.0 : 0;
        return g;
    });

    items.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
    return items;
}

function renderPriceAnalyticsTable(data) {
    const priceTbody = document.getElementById('analytics-tbody') || (els && els.tbodyAnalytics);
    if (!priceTbody) return;

    const items = processPriceData(data);
    if (data && data.length > 0 && (data[0].price_per_base_unit !== undefined || data[0].invoice_date !== undefined)) {
        analyticsCache = items;
    }

    if (!items || items.length === 0) {
        priceTbody.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">По вашему запросу ничего не найдено</td></tr>';
        return;
    }

    priceTbody.innerHTML = items.map(item => {
        const lastPrice = item.last_price !== undefined ? item.last_price : (item.latestPrice || 0);
        const medianPrice = item.median_price !== undefined ? item.median_price : (item.medianPrice || 0);
        item.last_price = lastPrice;
        item.median_price = medianPrice;

        let diff = item.price_diff || 0;
        let badgeHtml = '';
        if (diff > 0.5) {
            badgeHtml = `<span class="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg text-xs font-black bg-rose-950/50 text-rose-400 border border-rose-800/40">+${diff.toFixed(1)}% ↗</span>`;
        } else if (diff < -0.5) {
            badgeHtml = `<span class="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg text-xs font-black bg-emerald-950/50 text-emerald-400 border border-emerald-800/40">${diff.toFixed(1)}% ↘</span>`;
        } else {
            badgeHtml = `<span class="inline-flex items-center px-2 py-0.5 rounded text-[11px] font-bold text-slate-400 bg-slate-800/40">0.0%</span>`;
        }

        let latestColorClass = "text-slate-300";
        if (diff > 0.5) {
            latestColorClass = "text-rose-400";
        } else if (diff < -0.5) {
            latestColorClass = "text-emerald-400";
        }

        return `
            <tr class="hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle">
                <td class="p-4 align-middle">
                    <div class="font-bold text-slate-200 text-xs">${escapeHtml(item.name || '')}</div>
                    ${item.invoiceName && item.invoiceName !== item.name ? `<div class="text-[10px] text-slate-400 mt-0.5 truncate max-w-xs" title="${escapeHtml(item.invoiceName)}">В накладной: ${escapeHtml(item.invoiceName)}</div>` : ''}
                    ${item.uuid ? `<div class="text-[9px] text-slate-500 mt-0.5 font-mono">${escapeHtml(item.uuid)}</div>` : ''}
                </td>
                <td class="p-4 text-center border-r border-slate-800/40 align-middle">
                    <div class="font-bold ${latestColorClass} text-xs">${item.last_price.toFixed(2)} ₽ / ${escapeHtml(item.unit || 'ед.')}</div>
                    ${item.latestDate && item.latestDate !== '1970-01-01' ? `<div class="text-[9px] text-slate-500 mt-1">${item.latestDate}</div>` : ''}
                    ${item.latestSupplier ? `<div class="text-[9px] text-slate-400 mt-0.5">📦 ${escapeHtml(item.latestSupplier)}</div>` : ''}
                </td>
                <td class="p-4 text-center font-bold text-slate-300 text-xs border-r border-slate-800/40 align-middle">
                    ${item.median_price.toFixed(2)} ₽ / ${escapeHtml(item.unit || 'ед.')}
                </td>
                <td class="p-4 text-center align-middle">${badgeHtml}</td>
            </tr>
        `;
    }).join('');
}

function filterAnalytics() {
    const searchEl = document.getElementById('analytics-search') || (els && els.analyticsSearch);
    const query = searchEl ? searchEl.value.toLowerCase().trim() : '';
    if (!query) {
        renderPriceAnalyticsTable(analyticsCache);
        return;
    }
    const searchWords = query.split(/\s+/);
    const filtered = analyticsCache.filter(item => {
        const nameLower = (item.name || '').toLowerCase();
        const invLower = (item.invoiceName || '').toLowerCase();
        const latestSup = (item.latestSupplier || '').toLowerCase();
        return searchWords.every(word => 
            nameLower.includes(word) || invLower.includes(word) || latestSup.includes(word)
        );
    });
    renderPriceAnalyticsTable(filtered);
}

window.renderAnalytics = renderPriceAnalyticsTable;
window.renderPriceAnalyticsTable = renderPriceAnalyticsTable;
window.renderToxicWriteoffsTable = renderToxicWriteoffsTable;
window.filterAnalytics = filterAnalytics;
async function loadUnlistedOperations() {
    const companyId = els.company.value;
    if (!companyId) return;

    els.tbodyUnlisted.innerHTML = '<tr><td colspan="7" class="p-6 text-center text-slate-400">⏳ Загрузка списка операций...</td></tr>';

    try {
        const res = await fetch(`api/unlisted-operations?company_id=${companyId}`, {
            headers: { 'Authorization': getAuthToken() }
        });

        if (!res.ok) throw new Error(await res.text());

        const ops = await res.json() || [];

        if (ops.length === 0) {
            els.tbodyUnlisted.innerHTML = '<tr><td colspan="7" class="p-6 text-center text-slate-500 italic">Нет необработанных неучтенных списаний</td></tr>';
            return;
        }

        els.tbodyUnlisted.innerHTML = ops.map((op, idx) => {
            const dateObj = new Date(op.created_at);
            const dateStr = dateObj.toLocaleDateString('ru-RU') + ' ' + dateObj.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
            const inputId = `unlisted_match_${op.id}`;

            return `
                <tr class="hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle">
                    <td class="p-4 text-center text-slate-500 font-semibold border-r border-slate-800/40 align-middle">${idx + 1}</td>
                    <td class="p-4 align-middle">
                        <div class="font-bold text-slate-200">${escapeHtml(op.user_name)}</div>
                        <div class="text-[10px] text-slate-500 mt-1">${dateStr}</div>
                    </td>
                    <td class="p-4 font-bold text-rose-400 align-middle text-xs">
                        ⚠️ ${escapeHtml(op.position_name)}
                    </td>
                    <td class="p-4 text-center font-bold text-slate-300 text-xs align-middle">
                        ${op.quantity} ${escapeHtml(op.unit)}
                    </td>
                    <td class="p-4 text-slate-400 italic align-middle text-xs truncate max-w-[200px]" title="${escapeHtml(op.comment)}">
                        ${escapeHtml(op.comment) || '<span class="text-slate-600">Нет причины</span>'}
                    </td>
                    <td class="p-4 align-middle">
                        <input type="text" id="${inputId}" list="iiko-catalog-list" class="w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" placeholder="Введите имя товара в iiko...">
                    </td>
                    <td class="p-4 text-center align-middle whitespace-nowrap">
                        <button class="bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 border border-emerald-500/20 px-4 py-2 rounded-lg text-[10px] font-bold transition-all-300 shadow-sm" onclick="resolveUnlistedOperation(${op.id}, '${inputId}')">
                            Учесть в iiko
                        </button>
                        <button class="bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 px-3 py-2 rounded-lg text-[10px] font-bold transition-all-300 ml-2" onclick="rejectUnlistedOperation(${op.id})">🗑️ Отклонить</button>
                    </td>
                </tr>
            `;
        }).join('');

    } catch (err) {
        els.tbodyUnlisted.innerHTML = `<tr><td colspan="7" class="p-6 text-center text-rose-500 font-semibold">❌ Ошибка загрузки: ${err.message}</td></tr>`;
    }
}
async function resolveUnlistedOperation(opID, inputId) {
    const companyId = els.company.value;
    const input = document.getElementById(inputId);
    if (!input) return;

    const matchedName = input.value.trim();
    if (!matchedName) {
        alert("⚠️ Укажите правильное имя товара из iiko RMS!");
        return;
    }

    const cleanSearch = matchedName.toLowerCase().trim();
    const foundProduct = iikoCatalog.find(c => c.name.toLowerCase().trim() === cleanSearch);

    if (!foundProduct) {
        alert("❌ Товар с таким именем не найден в вашем справочнике iiko RMS! Пожалуйста, выберите корректное значение из выпадающего списка.");
        return;
    }

    if (!confirm(`Привязать операцию к товару из iiko?\n\nБыло: "Неучтенный товар"\nСтанет: "${foundProduct.name}"`)) {
        return;
    }

    try {
        const res = await fetch('api/unlisted-operations/resolve?token=' + getAuthToken(), {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                company_id: parseInt(companyId),
                operation_id: opID,
                iiko_product_name: foundProduct.name
            })
        });

        if (!res.ok) throw new Error(await res.text());

        alert("✅ Успешно сопоставлено! Списание приведено к номенклатуре и будет выгружено автоматически.");
        loadUnlistedOperations();
    } catch (err) {
        alert("❌ Ошибка сопоставления: " + err.message);
    }
}

async function rejectUnlistedOperation(opID) {
    if(!confirm("Отклонить списание? Оно будет удалено из этого списка.")) return;
    const cId = els.company.value;
    try {
        const res = await fetch(`api/unlisted-operations/reject?company_id=${cId}&operation_id=${opID}&token=${getAuthToken()}`, {method: 'DELETE'});
        if(res.ok) {
            loadUnlistedOperations();
        } else {
            alert("Ошибка удаления: " + await res.text());
        }
    } catch(e) { alert("Ошибка сети: " + e.message); }
}
window.rejectUnlistedOperation = rejectUnlistedOperation;