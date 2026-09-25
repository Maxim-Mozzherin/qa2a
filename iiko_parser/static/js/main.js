// ============================================================================


if (els.btnSubmitLogin) {
    els.btnSubmitLogin.addEventListener('click', async () => {
        const login = els.loginUser.value.trim();
        const password = els.loginPass.value.trim();

        if (!login || !password) {
            alert("Пожалуйста, заполните логин и пароль!");
            return;
        }

        try {
            const res = await fetch('api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ login, password })
            });

            if (!res.ok) {
                const errText = await res.text();
                throw new Error(errText || "Неверный логин или пароль (HTTP " + res.status + ")");
            }

            const data = await res.json();
            localStorage.setItem('bugh_token', data.token);
            if (data.role) localStorage.setItem("bugh_role", data.role);
            
            // Мгновенно активируем кнопку инвайта бухгалтера для роли superadmin без необходимости F5
            const btnAcc = document.getElementById("btn-generate-accountant-invite");
            if (btnAcc && data.role === "superadmin") {
                btnAcc.classList.remove("hidden");
            }

            els.loginUser.value = "";
            els.loginPass.value = "";

            checkAuthentication();
        } catch (err) {
            alert("❌ Ошибка входа: " + err.message);
        }
    });
}

if (els.btnLogout) {
    els.btnLogout.addEventListener('click', () => {
        if (confirm("Вы уверены, что хотите выйти из кабинета бухгалтера?")) {
            localStorage.removeItem('bugh_token');
            currentToken = "";
            currentDocData = null;
            iikoCatalog = [];
            iikoSuppliers = [];
            analyticsCache = [];
            location.reload();
        }
    });
}

document.addEventListener('DOMContentLoaded', checkAuthentication);

// ============================================================================
// 2. ИНИЦИАЛИЗАЦИЯ И ВЫБОР ЗАВЕДЕНИЯ
// ============================================================================

async function initDashboard() {
    try {
        const res = await fetch('api/companies', {
            headers: { 'Authorization': getAuthToken() }
        });

        if (res.status === 401) {
            localStorage.removeItem('bugh_token');
            checkAuthentication();
            return;
        }

        const companies = await res.json() || [];
        loadPromptPresets();
        els.company.innerHTML = '<option value="">-- Выберите заведение --</option>' +
            companies.map(c => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');

        const savedCompany = localStorage.getItem('active_company_id');
        if (savedCompany) {
            els.company.value = savedCompany;
            handleCompanyChange();
        }
        if (typeof startGlobalBadgePolling === 'function') startGlobalBadgePolling();
        if (typeof loadModelHealthBadge === 'function') loadModelHealthBadge();
    } catch (err) {
        console.error("Ошибка загрузки заведений:", err);
    }
}

if (els.company) {
    els.company.addEventListener('change', handleCompanyChange);
}







// ============================================================================
// 4. НЕЙРОСЕТЕВОЙ ПАРСИНГ УПД (PDF)
// ============================================================================

if (els.btnParse) {
    els.btnParse.addEventListener('click', (e) => {
        e.preventDefault();
        executeParseWithFiles(els.file.files);
    });
}


// ============================================================================
// DRAG AND DROP ОБРАБОТКА
// ============================================================================

['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
    window.addEventListener(eventName, (e) => {
        e.preventDefault();
        e.stopPropagation();
    }, false);
});

if (els.dropZone) {
    ['dragenter', 'dragover'].forEach(eventName => {
        els.dropZone.addEventListener(eventName, () => {
            els.dropZone.classList.add('border-brand-500', 'bg-brand-500/5');
        }, false);
    });

    ['dragleave', 'drop'].forEach(eventName => {
        els.dropZone.addEventListener(eventName, () => {
            els.dropZone.classList.remove('border-brand-500', 'bg-brand-500/5');
        }, false);
    });

    els.dropZone.addEventListener('drop', (e) => {
        const dt = e.dataTransfer;
        const files = dt.files;

        if (files.length > 0) {
            const file = files[0];
            const validExts = [".pdf", ".png", ".jpg", ".jpeg", ".webp"];
            const isValid = validExts.some(ext => file.name.toLowerCase().endsWith(ext)) || file.type.startsWith("image/") || file.type === "application/pdf";
            
            if (!isValid) {
                alert("Пожалуйста, выберите файл накладной (PDF или фото)");
                return;
            }

            els.file.files = files;
            executeParseWithFiles(files);
        }
    }, false);

    els.dropZone.addEventListener('click', (e) => {
        if (e.target.closest('button, select, input, a, label, #prompt-preset-select, #btn-open-prompt-modal')) {
            return;
        }
        els.file.click();
    });
}

// ============================================================================
// 5. ОТРИСОВКА СПЕЦИФИКАЦИИ И РАСЧЕТОВ НАКЛАДНОЙ
// ============================================================================


if (els.btnAddPage) {
    els.btnAddPage.addEventListener('click', (e) => {
        e.preventDefault();
        els.addFile.click();
    });
}
if (els.addFile) {
    els.addFile.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            executeAppendParseWithFiles(e.target.files);
        }
    });
}


window.updateFooterTotals = updateFooterTotals;

document.addEventListener('DOMContentLoaded', () => {
    const btnEmpty = document.getElementById('btn-create-empty');
    if (btnEmpty) btnEmpty.addEventListener('click', () => {
        executeAddEmptyRow();
        document.getElementById('results-section').classList.remove('hidden');
    });
    
    const btnAddRow = document.getElementById('btn-add-row');
    if (btnAddRow) btnAddRow.addEventListener('click', executeAddEmptyRow);
});

// ============================================================================
// 6. ИМПОРТ НАКЛАДНОЙ В IIKO RMS И ОБНОВЛЕНИЕ АНАЛИТИКИ
// ============================================================================

if (els.btnImport) {
    els.btnImport.addEventListener('click', async () => {
        if (!currentDocData) return;

        const companyId = els.company.value;
        const storeUuid = els.store.value;
        
        if (!companyId) return alert("Заведение не выбрано!");
        if (!storeUuid) return alert("⚠️ Сначала укажите Склад прихода в iiko!");

        const supplierName = els.supplierSearch.value.trim();
        const foundSupplier = iikoSuppliers.find(s => s.name === supplierName);
        const supplierUuid = foundSupplier ? foundSupplier.uuid : '';

        if (!supplierUuid) {
            alert("⚠️ Указанный Поставщик не найден! Пожалуйста, выберите корректного поставщика из выпадающего списка.");
            return;
        }

        const rows = els.tbody.querySelectorAll('.invoice-item-row');
        if (rows.length === 0 || !currentDocData.items || currentDocData.items.length === 0) {
            alert("⚠️ Нет товаров для отправки!");
            return;
        }

        const itemsToImport = [];
        let hasErrors = false;
        let unmappedCount = 0;
        let firstErrorEl = null;

        rows.forEach((tr, idx) => {
            const searchInputEl = tr.querySelector('.iiko-search');
            const searchInput = searchInputEl ? searchInputEl.value.trim() : "";
            const originalItem = currentDocData.items[idx];
            if (!originalItem) return;

            let multInput = (typeof originalItem.multiplier === 'number') ? originalItem.multiplier : 1.0;
            const finalQtyInput = tr.querySelector('.iiko-final-qty');
            if (finalQtyInput) {
                const finalQ = parseFloat(finalQtyInput.value.replace(',', '.')) || 0;
                const q = parseFloat(originalItem.quantity) || 0;
                if (q > 0) multInput = finalQ / q;
            } else {
                const rawMult = (tr.querySelector('.iiko-mult')?.value || "").replace(',', '.');
                if (rawMult) multInput = parseFloat(rawMult) || multInput;
            }

            let mappedUuid = "";
            let mappedName = searchInput;

            if (searchInput !== "") {
                const uuidRegex = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
                const match = searchInput.match(uuidRegex);

                if (match) {
                    mappedUuid = match[0];
                    mappedName = `[UUID] ${searchInput}`;
                } else {
                    const cleanSearch = searchInput.toLowerCase().trim();
                    const foundProduct = iikoCatalog.find(c =>
                        c.name.toLowerCase().trim() === cleanSearch ||
                        c.uuid.toLowerCase().trim() === cleanSearch
                    );

                    if (foundProduct) {
                        mappedUuid = foundProduct.uuid;
                        mappedName = foundProduct.name;
                    }
                }
            }

            if (!mappedUuid) {
                hasErrors = true;
                unmappedCount++;
                if (searchInputEl) {
                    searchInputEl.classList.add('border-red-500/80', 'bg-red-500/10', 'ring-2', 'ring-red-500/20');
                    if (!firstErrorEl) firstErrorEl = searchInputEl;
                }
            } else {
                if (searchInputEl) {
                    searchInputEl.classList.remove('border-red-500/80', 'bg-red-500/10', 'ring-2', 'ring-red-500/20');
                }
                
                const userCategory = tr.querySelector('.clean-category-select')?.value || originalItem.clean_category || "";
                const baseUnitInput = tr.querySelector('.iiko-base-unit-input')?.value.trim() || originalItem.base_unit || originalItem.unit || "кг/шт";
                itemsToImport.push({
                    name: originalItem.name,
                    clean_category: userCategory,
                    brand: originalItem.brand || "",
                    quantity: originalItem.quantity,
                    unit: baseUnitInput,
                    price: originalItem.price,
                    sum: originalItem.sum,
                    sum_without_nds: parseFloat(originalItem.sum_without_nds) || 0.0,
                    nds_percent: parseFloat(originalItem.nds_percent) || 0.0,
                    mapped_uuid: mappedUuid,
                    mapped_name: mappedName,
                    multiplier: multInput
                });
            }
        });

        if (hasErrors || unmappedCount > 0) {
            if (firstErrorEl) {
                firstErrorEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
                firstErrorEl.focus();
            }
            alert(`⛔ Отправка накладной запрещена!\n\nНе все позиции сопоставлены с номенклатурой iiko RMS (не привязано позиций: ${unmappedCount} из ${rows.length}).\n\nВсе товары из накладной обязательно должны быть сопоставлены со справочником iiko. Пожалуйста, укажите номенклатуру для строк, подсвеченных красным, или удалите лишние позиции (кнопка ✕), перед тем как отправить документ.`);
            return;
        }

        if (itemsToImport.length === 0) {
            alert("⚠️ Нет товаров для отправки!");
            return;
        }

        const payload = {
            company_id: parseInt(companyId),
            store_uuid: storeUuid,
            supplier_uuid: supplierUuid,
            vendor_name: currentDocData.vendor_name,
            vendor_inn: currentDocData.vendor_inn || "",
            consignee: currentDocData.consignee,
            shipper: currentDocData.shipper,
            invoice_number: currentDocData.doc_number,
            invoice_date: els.resDocdate ? els.resDocdate.value.trim() : "",
            items: itemsToImport
        };

        els.btnImport.disabled = true;
        els.loaderImport.classList.remove('hidden');

        try {
            const res = await fetch('api/import', {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json',
                    'Authorization': 'Bearer ' + getAuthToken()
                },
                body: JSON.stringify(payload)
            });

            const text = await res.text();
            if (!res.ok) throw new Error(text);

            alert("✅ Успешно! Накладная создана в iiko RMS и добавлена в историю аналитики.");
            els.resSection.classList.add('hidden');
            els.file.value = "";
            currentDocData = null; // Сброс состояния для предотвращения случайного прикрепления фото к старой накладной
            
            // Если вкладка аналитики загружалась, обновляем её данными новой накладной
            if (analyticsCache.length > 0) {
                loadAnalytics();
            }

        } catch (err) {
            alert("❌ Ошибка отправки:\n" + err.message);
        } finally {
            els.btnImport.disabled = false;
            els.loaderImport.classList.add('hidden');
        }
    });
}

window.deleteInvoiceItem = deleteInvoiceItem;

// ============================================================================
// 7. АНАЛИТИКА ЦЕН И ЗАКУПОК (DASHBOARD)
// ============================================================================



window.loadAnalytics = loadAnalytics;
window.filterAnalytics = filterAnalytics;


// ============================================================================
// 8. ДИСПЕТЧЕР НЕУЧТЕННЫХ СПИСАНИЙ
// ============================================================================


if (els.btnRefreshUnlisted) {
    els.btnRefreshUnlisted.addEventListener('click', loadUnlistedOperations);
}


window.resolveUnlistedOperation = resolveUnlistedOperation;
window.loadUnlistedOperations = loadUnlistedOperations;

// ============================================================================
// 9. ВКЛАДКИ (TABS)
// ============================================================================


window.switchBughTab = switchBughTab;

// ============================================================================
// 10. КОНСТРУКТОР БЛАНКОВ ИНВЕНТАРИЗАЦИИ
// ============================================================================

const btnAddTplItem = document.getElementById('btn-add-tpl-item');
if (btnAddTplItem) {
    btnAddTplItem.addEventListener('click', () => {
        const input = document.getElementById('tpl-search');
        const val = input.value.trim();
        if (!val) return;

        const found = iikoCatalog.find(c => c.name === val);
        if (!found) {
            alert("⚠️ Товар не найден в справочнике! Пожалуйста, выберите товар из выпадающего списка.");
            return;
        }

        if (templateItems.includes(val)) {
            alert("Товар уже добавлен в бланк!");
            return;
        }

        templateItems.push(val);
        input.value = '';
        renderTemplateTable();
    });
}



const btnSaveTemplate = document.getElementById('btn-save-template');
if (btnSaveTemplate) {
    btnSaveTemplate.addEventListener('click', async () => {
        const name = document.getElementById('tpl-name').value.trim();
        const companyId = els.company.value;
        const storeUuid = els.store.value;

        if (!companyId) return alert("⚠️ Выберите заведение!");
        if (!storeUuid) return alert("⚠️ Выберите склад в настройках выше!");
        if (!name) return alert("⚠️ Укажите название бланка (например: Пятница Бар Срез)!");
        if (templateItems.length === 0) return alert("⚠️ Добавьте хотя бы один товар в бланк!");

        const oldText = btnSaveTemplate.innerText;
        btnSaveTemplate.disabled = true;
        btnSaveTemplate.innerText = "⏳ Опубликование бланка...";

        const payload = {
            store_uuid: storeUuid,
            name: name,
            items: templateItems,
            company_id: parseInt(companyId)
        };

        try {
            const res = await fetch('api/templates/save', {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json',
                    'Authorization': 'Bearer ' + getAuthToken()
                },
                body: JSON.stringify(payload)
            });

            if (!res.ok) throw new Error(await res.text());

            alert("✅ Успешно! Бланк сохранен и опубликован в Mini App для сотрудников.");

            document.getElementById('tpl-name').value = '';
            templateItems = [];
            renderTemplateTable();
        } catch (err) {
            alert("❌ Ошибка сохранения:\n" + err.message);
        } finally {
            btnSaveTemplate.disabled = false;
            btnSaveTemplate.innerText = oldText;
        }
    });
}


renderTemplateTable();
window.removeTemplateItem = removeTemplateItem;
// ============================================================================
// 10. Resizable Columns
// ============================================================================

document.addEventListener('DOMContentLoaded', () => {
    initResizableColumns('items-tbody');
});



// ============================================================================
// УПРАВЛЕНИЕ КАСТОМНЫМИ ПРОМПТАМИ И ПРЕСЕТАМИ AI
// ============================================================================











// Привязка событий пресетов
if (els.promptPresetSelect) {
    els.promptPresetSelect.addEventListener('change', (e) => {
        selectPresetById(e.target.value);
    });
}
if (els.modalPresetSelect) {
    els.modalPresetSelect.addEventListener('change', (e) => {
        selectPresetById(e.target.value);
    });
}
if (els.btnOpenPromptModal) {
    els.btnOpenPromptModal.addEventListener('click', (e) => {
        e.preventDefault();
        openPromptModal();
    });
}
if (els.btnClosePromptModal) {
    els.btnClosePromptModal.addEventListener('click', closePromptModal);
}
if (els.btnCancelPromptModal) {
    els.btnCancelPromptModal.addEventListener('click', closePromptModal);
}
if (els.btnSavePreset) {
    els.btnSavePreset.addEventListener('click', savePromptPreset);
}
if (els.btnDeletePreset) {
    els.btnDeletePreset.addEventListener('click', deletePromptPreset);
}
if (els.btnNewPreset) {
    els.btnNewPreset.addEventListener('click', createNewPresetForm);
}
if (els.btnResetDefaultPrompt) {
    els.btnResetDefaultPrompt.addEventListener('click', resetToDefaultPrompt);
}
if (els.modalPromptTextarea) {
    els.modalPromptTextarea.addEventListener('input', updatePromptCharCount);
}




// Кнопка 1: Инвайт для ресторана (доступна всем бухгалтерам)
const btnInviteCompany = document.getElementById("btn-generate-invite");
if (btnInviteCompany) {
    btnInviteCompany.addEventListener("click", async () => {
        const cmpName = prompt("Введите название заведения для инвайта:", "Новое заведение");
        if (!cmpName) return;
        try {
            const res = await fetch("api/invite/generate", { 
                method: "POST", 
                headers: {
                    "Content-Type": "application/json",
                    "Authorization": "Bearer " + getAuthToken()
                }, 
                body: JSON.stringify({name: cmpName}) 
            });
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            
            try {
                await navigator.clipboard.writeText(data.invite_code);
                alert("✅ Код заведения скопирован в буфер обмена!\n\n" + data.invite_code + "\n\nПередайте его управляющему заведения.");
            } catch (e) {
                prompt("Скопируйте инвайт-код вручную:", data.invite_code);
            }
            location.reload();
        } catch(err) {
            alert("❌ Ошибка:\n" + err.message);
        }
    });
}

// Кнопка 2: Инвайт для бухгалтера (видна только роли admin)
const btnInviteAccountant = document.getElementById("btn-generate-accountant-invite");
if (btnInviteAccountant) {
    // Проверяем видимость при старте
    if (localStorage.getItem("bugh_role") === "superadmin") {
        btnInviteAccountant.classList.remove("hidden");
    }

    btnInviteAccountant.addEventListener("click", async () => {
        try {
            btnInviteAccountant.innerText = "⏳ Генерация...";
            const res = await fetch("api/accountant-invite/generate", { 
                method: "POST",
                headers: {
                    "Authorization": "Bearer " + getAuthToken()
                }
            });
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            
            try {
                await navigator.clipboard.writeText(data.invite_link);
                alert("✅ Ссылка скопирована в буфер обмена!\n\n" + data.invite_link + "\n\nОна действительна 24 часа. Отправьте ее новому бухгалтеру!");
            } catch (e) {
                prompt("Скопируйте ссылку вручную:", data.invite_link);
            }
        } catch(err) {
            alert("❌ Ошибка:\n" + err.message);
        } finally {
            btnInviteAccountant.innerHTML = "👨💼 Пригласить бухгалтера";
        }
    });
}
document.addEventListener("DOMContentLoaded", () => {
    const registerModal = document.getElementById("registerModal");
    const registerForm = document.getElementById("registerForm");
    const urlParams = new URLSearchParams(window.location.search);
    const inviteCodeParam = urlParams.get("invite");

    if (inviteCodeParam) {
        if (els.loginModal) els.loginModal.classList.add("hidden");
        if (registerModal) registerModal.classList.remove("hidden");
    }

    if (registerForm) {
        registerForm.addEventListener("submit", async (e) => {
            e.preventDefault();
            const login = document.getElementById("reg-login").value.trim();
            const email = document.getElementById("reg-email").value.trim();
            const password = document.getElementById("reg-password").value.trim();

            if (!login || !password) return alert("Заполните логин и пароль");

            const btn = registerForm.querySelector("button");
            const oldText = btn.innerText;
            btn.innerText = "⏳ Создание аккаунта...";
            btn.disabled = true;

            try {
                const res = await fetch("api/accountant-invite/register", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ invite_code: inviteCodeParam, login, email, password })
                });

                if (!res.ok) throw new Error(await res.text());
                
                alert("✅ Успешно! Аккаунт бухгалтера создан.\n\nТеперь вы можете войти в систему под своими данными.");
                window.location.href = window.location.pathname; // Remove ?invite=
            } catch(err) {
                alert("❌ Ошибка:\n" + err.message);
            } finally {
                btn.innerText = oldText;
                btn.disabled = false;
            }
        });
    }
});
