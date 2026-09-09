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
    } catch (err) {
        console.error("Ошибка загрузки заведений:", err);
    }
}

if (els.company) {
    els.company.addEventListener('change', handleCompanyChange);
}




if (els.btnSave) {
    els.btnSave.addEventListener('click', () => {
        const companyId = els.company.value;
        const storeUuid = els.store.value;
        const supplierName = els.supplierSearch.value.trim();

        if (!companyId) return alert("Пожалуйста, выберите заведение!");

        localStorage.setItem(`saved_store_${companyId}`, storeUuid);
        localStorage.setItem(`saved_supplier_name_${companyId}`, supplierName);
        
        const foundSupplier = iikoSuppliers.find(s => s.name === supplierName);
        if (foundSupplier) {
            localStorage.setItem(`saved_supplier_uuid_${companyId}`, foundSupplier.uuid);
        }

        alert('💾 Настройки сопоставления сохранены локально!');
    });
}

// ============================================================================
// 3. ОБНОВЛЕНИЕ СПРАВОЧНИКОВ IIKO RMS
// ============================================================================

if (els.btnCatalog) {
    els.btnCatalog.addEventListener('click', async () => {
        const companyId = els.company.value;
        if (!companyId) {
            alert("Пожалуйста, сначала выберите активное заведение!");
            return;
        }

        els.btnCatalog.disabled = true;
        els.btnCatalog.innerText = "⏳ Синхронизация...";

        try {
            const res = await fetch('api/catalog', {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json',
                    'Authorization': getAuthToken()
                },
                body: JSON.stringify({ company_id: parseInt(companyId) })
            });

            if (!res.ok) throw new Error(await res.text());

            const data = await res.json();
            currentToken = data.token;
            iikoCatalog = data.catalog || [];
            iikoSuppliers = data.suppliers || [];

            localStorage.setItem(`cached_catalog_${companyId}`, JSON.stringify(iikoCatalog));
            localStorage.setItem(`cached_suppliers_${companyId}`, JSON.stringify(iikoSuppliers));

            populateDatalists();

            if (data.stores && data.stores.length > 0) {
                els.store.innerHTML = '';
                data.stores.forEach(s => {
                    const opt = document.createElement('option');
                    opt.value = s.uuid;
                    opt.textContent = s.name;
                    els.store.appendChild(opt);
                });
                els.store.disabled = false;

                const savedStore = localStorage.getItem(`saved_store_${companyId}`);
                if (savedStore) els.store.value = savedStore;
            }

            els.supplierSearch.disabled = false;
            const savedSupplierName = localStorage.getItem(`saved_supplier_name_${companyId}`);
            if (savedSupplierName) els.supplierSearch.value = savedSupplierName;

            els.badge.innerText = `✅ Справочник: ${iikoCatalog.length} товаров`;
            els.badge.className = "px-3 py-1 rounded-full text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20";
            
            els.btnParse.disabled = false;
            els.btnParse.classList.remove('opacity-50', 'cursor-not-allowed');

            loadUnlistedOperations();

        } catch (err) {
            alert("❌ Ошибка соединения: " + err.message);
        } finally {
            els.btnCatalog.disabled = false;
            els.btnCatalog.innerHTML = "🔄 Обновить справочник iiko";
        }
    });
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
                alert("����������, �������� ���� ��������� (PDF ��� ����)");
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

        const rows = els.tbody.querySelectorAll('tr');
        const itemsToImport = [];
        let hasErrors = false;

        rows.forEach((tr, idx) => {
            const searchInput = tr.querySelector('.iiko-search')?.value.trim();
            const rawMult = (tr.querySelector('.iiko-mult')?.value || "").replace(',', '.');
            const multInput = parseFloat(rawMult) || 1.0;
            const originalItem = currentDocData.items[idx];

            if (searchInput && searchInput !== "") {
                let mappedUuid = "";
                let mappedName = searchInput;

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

                if (!mappedUuid) {
                    hasErrors = true;
                    tr.querySelector('.iiko-search').classList.add('border-red-500/80', 'bg-red-500/5');
                } else {
                    tr.querySelector('.iiko-search').classList.remove('border-red-500/80', 'bg-red-500/5');
                    
                    const userCategory = tr.querySelector('.clean-category-select')?.value || originalItem.clean_category || "";
                    itemsToImport.push({
                        name: originalItem.name,
                        clean_category: userCategory,
                        brand: originalItem.brand || "",
                        quantity: originalItem.quantity,
                        price: originalItem.price,
                        sum: originalItem.sum,
                        sum_without_nds: parseFloat(originalItem.sum_without_nds) || 0.0,
                        nds_percent: parseFloat(originalItem.nds_percent) || 0.0,
                        mapped_uuid: mappedUuid,
                        mapped_name: mappedName,
                        multiplier: multInput
                    });
                }
            }
        });

        if (hasErrors) {
            alert("⚠️ Некоторые товары не сопоставлены со справочником! Проверьте поля, подсвеченные красным.");
            return;
        }

        if (itemsToImport.length === 0) {
            alert("⚠️ Нет товаров для отправки!");
            return;
        }

        const payload = {
            company_id: parseInt(companyId),
            token: currentToken,
            store_uuid: storeUuid,
            supplier_uuid: supplierUuid,
            vendor_name: currentDocData.vendor_name,
            consignee: currentDocData.consignee,
            shipper: currentDocData.shipper,
            invoice_number: currentDocData.doc_number,
            invoice_date: els.resDocdate ? els.resDocdate.value.trim() : "",
            items: itemsToImport
        };

        els.btnImport.disabled = true;
        els.loaderImport.classList.remove('hidden');

        try {
            const res = await fetch('api/import?token=' + getAuthToken(), {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(payload)
            });

            const text = await res.text();
            if (!res.ok) throw new Error(text);

            alert("✅ Успешно! Накладная создана в iiko RMS и добавлена в историю аналитики.");
            els.resSection.classList.add('hidden');
            els.file.value = "";
            
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
            const res = await fetch('api/templates/save?token=' + getAuthToken(), {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json'
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

