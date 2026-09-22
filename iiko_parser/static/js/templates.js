function renderTemplateTable() {
    const tbody = document.getElementById('tpl-tbody');
    if (!tbody) return;
    tbody.innerHTML = '';

    if (templateItems.length === 0) {
        tbody.innerHTML = `<tr><td colspan="3" class="p-6 text-center text-slate-500 italic">Добавьте товары в список через строку поиска выше</td></tr>`;
        return;
    }

    templateItems.forEach((name, idx) => {
        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40";
        tr.innerHTML = `
            <td class="p-4 text-center text-slate-500 font-bold border-r border-slate-800/40 align-middle">${idx + 1}</td>
            <td class="p-4 font-bold text-slate-100 text-xs align-middle">${escapeHtml(name)}</td>
            <td class="p-4 text-center align-middle">
                <button class="bg-rose-500/10 text-rose-400 border border-rose-500/20 px-3 py-1.5 rounded-lg hover:bg-rose-500/20 transition-all-300 text-xs font-semibold" onclick="removeTemplateItem(${idx})">
                    Удалить
                </button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}
function removeTemplateItem(idx) {
    templateItems.splice(idx, 1);
    renderTemplateTable();
}