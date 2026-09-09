async function loadAnalytics() {
    const companyId = els.company.value;
    if (!companyId) return;

    const days = els.analyticsDays ? els.analyticsDays.value : 30;
    els.tbodyAnalytics.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-400 font-semibold animate-pulse">⏳ Вычисляем аналитику...</td></tr>';

    try {
        const res = await fetch(`api/analytics?company_id=${companyId}&days=${days}`, {
            headers: { 'Authorization': getAuthToken() }
        });

        if (!res.ok) throw new Error(await res.text());

        const records = await res.json() || [];

        if (records.length === 0) {
            els.tbodyAnalytics.innerHTML = `<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">За последние ${days} дней накладные не проводились</td></tr>`;
            analyticsCache = [];
            return;
        }

        // Группировка и вычисления на лету (Client-side OLAP)
        const grouped = {};
        
        records.forEach(r => {
            const key = (r.iiko_product_uuid && r.iiko_product_uuid.trim() !== "") ? r.iiko_product_uuid : (r.iiko_product_name || r.product_name);
            const displayName = r.iiko_product_name || r.product_name;
            if (!grouped[key]) {
                grouped[key] = {
                    name: displayName,
                    invoiceName: r.product_name,
                    uuid: r.iiko_product_uuid || "",
                    prices: [],
                    latestDate: '1970-01-01',
                    latestPrice: 0,
                    latestSupplier: '',
                    minPrice: Infinity,
                    bestSupplier: ''
                };
            }

            const price = parseFloat(r.price_per_base_unit);
            grouped[key].prices.push(price);

            // Ищем последнюю цену по дате накладной
            if (r.invoice_date >= grouped[key].latestDate) {
                grouped[key].latestDate = r.invoice_date;
                grouped[key].latestPrice = price;
                grouped[key].latestSupplier = r.supplier_name;
            }

            // Ищем исторический минимум за период
            if (price < grouped[key].minPrice) {
                grouped[key].minPrice = price;
                grouped[key].bestSupplier = r.supplier_name;
            }
        });

        analyticsCache = Object.values(grouped).map(g => {
            // Расчет медианы (сортируем цены и берем среднюю точку)
            g.prices.sort((a, b) => a - b);
            const mid = Math.floor(g.prices.length / 2);
            if (g.prices.length % 2 !== 0) {
                g.medianPrice = g.prices[mid];
            } else {
                g.medianPrice = (g.prices[mid - 1] + g.prices[mid]) / 2.0;
            }
            return g;
        });

        // Сортировка по алфавиту для удобства
        analyticsCache.sort((a, b) => a.name.localeCompare(b.name));

        renderAnalytics(analyticsCache);

    } catch (err) {
        els.tbodyAnalytics.innerHTML = `<tr><td colspan="4" class="p-6 text-center text-rose-500 font-semibold">❌ Ошибка загрузки: ${err.message}</td></tr>`;
    }
}
function renderAnalytics(data) {
    if (data.length === 0) {
        els.tbodyAnalytics.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">По вашему запросу ничего не найдено</td></tr>';
        return;
    }

    els.tbodyAnalytics.innerHTML = data.map(item => {
        let latestColorClass = "text-slate-300";
        let icon = "";
        let tooltip = "Цена в норме";

        if (item.latestPrice > item.medianPrice * 1.05) {
            latestColorClass = "text-rose-400";
            icon = "↑";
            tooltip = "Товар подорожал относительно медианы";
        } else if (item.latestPrice < item.medianPrice * 0.95) {
            latestColorClass = "text-emerald-400";
            icon = "↓";
            tooltip = "Товар подешевел относительно медианы";
        }

        return `
            <tr class="hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle">
                <td class="p-4 align-middle">
                    <div class="font-bold text-slate-200 text-xs">${escapeHtml(item.name)}</div>
                    ${item.invoiceName && item.invoiceName !== item.name ? `<div class="text-[10px] text-slate-400 mt-0.5 truncate max-w-xs" title="${escapeHtml(item.invoiceName)}">В накладной: ${escapeHtml(item.invoiceName)}</div>` : ''}
                    ${item.uuid ? `<div class="text-[9px] text-slate-500 mt-0.5 font-mono">${escapeHtml(item.uuid)}</div>` : ''}
                </td>
                <td class="p-4 text-center border-r border-slate-800/40 align-middle" title="${tooltip}">
                    <div class="font-bold ${latestColorClass} text-xs">${item.latestPrice.toFixed(2)} ₽ ${icon}</div>
                    <div class="text-[9px] text-slate-500 mt-1">${item.latestDate}</div>
                    ${item.latestSupplier ? `<div class="text-[9px] text-slate-400 mt-0.5">📦 ${escapeHtml(item.latestSupplier)}</div>` : ''}
                </td>
                <td class="p-4 text-center font-bold text-slate-300 text-xs border-r border-slate-800/40 align-middle">
                    ${item.medianPrice.toFixed(2)} ₽
                </td>
                <td class="p-4 align-middle">
                    <div class="font-bold text-emerald-400 text-xs">${item.minPrice.toFixed(2)} ₽</div>
                    <div class="text-[10px] text-slate-400 mt-1 whitespace-normal break-words">🏆 ${escapeHtml(item.bestSupplier)}</div>
                </td>
            </tr>
        `;
    }).join('');
}
function filterAnalytics() {
    const query = els.analyticsSearch ? els.analyticsSearch.value.toLowerCase().trim() : '';
    if (!query) {
        renderAnalytics(analyticsCache);
        return;
    }
    const searchWords = query.split(/\s+/);
    const filtered = analyticsCache.filter(item => {
        const nameLower = (item.name || '').toLowerCase();
        const invLower = (item.invoiceName || '').toLowerCase();
        const bestSup = (item.bestSupplier || '').toLowerCase();
        const latestSup = (item.latestSupplier || '').toLowerCase();
        return searchWords.every(word => 
            nameLower.includes(word) || invLower.includes(word) || bestSup.includes(word) || latestSup.includes(word)
        );
    });
    renderAnalytics(filtered);
}
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
                        ${op.quantity} ${op.unit}
                    </td>
                    <td class="p-4 text-slate-400 italic align-middle text-xs truncate max-w-[200px]" title="${escapeHtml(op.comment)}">
                        ${escapeHtml(op.comment) || '<span class="text-slate-600">Нет причины</span>'}
                    </td>
                    <td class="p-4 align-middle">
                        <input type="text" id="${inputId}" list="iiko-catalog-list" class="w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" placeholder="Введите имя товара в iiko...">
                    </td>
                    <td class="p-4 text-center align-middle">
                        <button class="bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 border border-emerald-500/20 px-4 py-2 rounded-lg text-[10px] font-bold transition-all-300 shadow-sm" onclick="resolveUnlistedOperation(${op.id}, '${inputId}')">
                            Учесть в iiko
                        </button>
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