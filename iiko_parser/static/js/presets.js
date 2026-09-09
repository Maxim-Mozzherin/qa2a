async function loadPromptPresets(companyId) {
    try {
        const cId = companyId || (els.company ? els.company.value : "") || "0";
        const res = await fetch('api/parser/presets?company_id=' + cId + '&token=' + getAuthToken());
        if (!res.ok) return;

        promptPresets = await res.json() || [];
        renderPresetDropdowns();

        const savedPresetId = localStorage.getItem('active_prompt_preset_id');
        let selectedPreset = promptPresets.find(p => String(p.id) === String(savedPresetId));
        if (!selectedPreset && promptPresets.length > 0) {
            selectedPreset = promptPresets[0];
        }

        if (selectedPreset) {
            selectPresetById(selectedPreset.id);
        }
    } catch (err) {
        console.error("Ошибка загрузки пресетов промптов:", err);
    }
}
function renderPresetDropdowns() {
    const pSel = document.getElementById('prompt-preset-select');
    const mSel = document.getElementById('modal-preset-select');

    const presetsToRender = (promptPresets && promptPresets.length > 0) ? promptPresets : [
        { id: 1, name: "Стандартный (УПД / ТОРГ-12)", is_default: true },
        { id: 2, name: "Многостраничная накладная (фото / сканы)", is_default: true },
        { id: 3, name: "Товарный чек / Простая квитанция", is_default: true }
    ];

    const optionsHtml = presetsToRender.map(p => {
        const prefix = p.is_default ? "⭐ " : "📁 ";
        return '<option value="' + p.id + '">' + prefix + escapeHtml(p.name) + '</option>';
    }).join('');

    if (pSel) pSel.innerHTML = optionsHtml;
    if (mSel) mSel.innerHTML = optionsHtml;
}
function selectPresetById(presetId) {
    const preset = (promptPresets || []).find(p => String(p.id) === String(presetId));
    if (!preset) return;

    activePresetId = preset.id;
    currentCustomPrompt = preset.prompt || "";
    localStorage.setItem('active_prompt_preset_id', preset.id);

    const pSel = document.getElementById('prompt-preset-select');
    const mSel = document.getElementById('modal-preset-select');
    const mName = document.getElementById('modal-preset-name');
    const mText = document.getElementById('modal-prompt-textarea');
    const badge = document.getElementById('preset-badge');
    const btnDel = document.getElementById('btn-delete-preset');

    if (pSel) pSel.value = preset.id;
    if (mSel) mSel.value = preset.id;
    if (mName) mName.value = preset.name;
    if (mText) {
        mText.value = preset.prompt || "";
        updatePromptCharCount();
    }

    if (badge) {
        if (preset.is_default) {
            badge.textContent = "Системный шаблон (защищен)";
            badge.className = "px-2.5 py-1 bg-brand-500/10 border border-brand-500/20 text-brand-400 text-[10px] font-semibold rounded-lg";
        } else {
            badge.textContent = "Пользовательский пресет";
            badge.className = "px-2.5 py-1 bg-amber-500/10 border border-amber-500/20 text-amber-400 text-[10px] font-semibold rounded-lg";
        }
    }

    if (btnDel) {
        if (preset.is_default) {
            btnDel.disabled = true;
            btnDel.classList.add('opacity-40', 'cursor-not-allowed');
            btnDel.title = "Системный шаблон нельзя удалить";
        } else {
            btnDel.disabled = false;
            btnDel.classList.remove('opacity-40', 'cursor-not-allowed');
            btnDel.title = "Удалить пользовательский пресет";
        }
    }
}
function updatePromptCharCount() {
    if (!els.promptCharCount || !els.modalPromptTextarea) return;
    const len = els.modalPromptTextarea.value.length;
    els.promptCharCount.textContent = len + ' симв.';
}
function openPromptModal() {
    if (!els.promptModal) return;
    if (activePresetId) {
        selectPresetById(activePresetId);
    }
    els.promptModal.classList.remove('hidden');
}
function closePromptModal() {
    if (!els.promptModal) return;
    els.promptModal.classList.add('hidden');
}
async function savePromptPreset() {
    const name = els.modalPresetName.value.trim();
    const promptText = els.modalPromptTextarea.value.trim();

    if (!name) {
        alert("Пожалуйста, введите название пресета!");
        els.modalPresetName.focus();
        return;
    }
    if (!promptText) {
        alert("Текст промпта не может быть пустым!");
        els.modalPromptTextarea.focus();
        return;
    }

    const companyId = parseInt(els.company ? els.company.value : "0") || 0;
    const payload = {
        id: activePresetId || 0,
        company_id: companyId,
        name: name,
        prompt: promptText
    };

    try {
        const res = await fetch('api/parser/presets?token=' + getAuthToken(), {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!res.ok) throw new Error(await res.text());

        const result = await res.json();
        const targetId = result.id || activePresetId;

        await loadPromptPresets(companyId);
        selectPresetById(targetId);

        currentCustomPrompt = promptText;
        closePromptModal();

        if (result.is_copy) {
            alert('✅ Так как системный шаблон защищен, создана его пользовательская копия: "' + name + ' (Копия)" и выбрана в качестве активной.');
        } else {
            alert('✅ Пресет "' + name + '" успешно сохранен и применен!');
        }
    } catch (err) {
        alert("❌ Ошибка сохранения пресета: " + err.message);
    }
}
async function deletePromptPreset() {
    if (!activePresetId) return;
    const preset = promptPresets.find(p => p.id === activePresetId);
    if (!preset || preset.is_default) {
        alert("Нельзя удалить системный шаблон.");
        return;
    }

    if (!confirm('Вы действительно хотите удалить пресет "' + preset.name + '"?')) {
        return;
    }

    try {
        const res = await fetch('api/parser/presets?id=' + activePresetId + '&token=' + getAuthToken(), {
            method: 'DELETE'
        });

        if (!res.ok) throw new Error(await res.text());

        localStorage.removeItem('active_prompt_preset_id');
        const companyId = parseInt(els.company ? els.company.value : "0") || 0;
        await loadPromptPresets(companyId);
        alert("🗑️ Пресет успешно удален.");
    } catch (err) {
        alert("❌ Ошибка удаления пресета: " + err.message);
    }
}
async function resetToDefaultPrompt() {
    try {
        const res = await fetch('api/parser/default-prompt?token=' + getAuthToken());
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        if (data.prompt && els.modalPromptTextarea) {
            els.modalPromptTextarea.value = data.prompt;
            updatePromptCharCount();
        }
    } catch (err) {
        alert("Ошибка сброса промпта: " + err.message);
    }
}
function createNewPresetForm() {
    activePresetId = 0;
    if (els.modalPresetName) {
        els.modalPresetName.value = "Новый пресет";
        els.modalPresetName.focus();
        els.modalPresetName.select();
    }
    if (els.presetBadge) {
        els.presetBadge.textContent = "Новый пресет (не сохранен)";
        els.presetBadge.className = "px-2.5 py-1 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-[10px] font-semibold rounded-lg";
    }
    if (els.btnDeletePreset) {
        els.btnDeletePreset.disabled = true;
        els.btnDeletePreset.classList.add('opacity-40', 'cursor-not-allowed');
    }
}