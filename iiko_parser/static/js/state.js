// ============================================================================
// ГЛОБАЛЬНЫЕ СЕЛЕКТОРЫ И СОСТОЯНИЕ ПРИЛОЖЕНИЯ
// ============================================================================

const els = {
    loginModal: document.getElementById('login-modal'),
    dashboardContent: document.getElementById('dashboard-content'),
    loginUser: document.getElementById('login-user'),
    loginPass: document.getElementById('login-pass'),
    btnSubmitLogin: document.getElementById('btn-submit-login'),
    btnLogout: document.getElementById('btn-logout'),

    company: document.getElementById('set-company'),
    store: document.getElementById('set-store'),
    supplierSearch: document.getElementById('set-supplier-search'),
    supplierDatalist: document.getElementById('iiko-suppliers-list'),
    btnSave: document.getElementById('btn-save-settings'),
    badge: document.getElementById('status-badge'),
    datalist: document.getElementById('iiko-catalog-list'),

    file: document.getElementById('pdf-file'),
    btnParse: document.getElementById('btn-parse'),
    loaderParse: document.getElementById('parse-loader'),
    dropZone: document.getElementById('drop-zone'),
    addFile: document.getElementById('pdf-add-file'),
    btnAddPage: document.getElementById('btn-add-page'),
    loaderAddPage: document.getElementById('add-page-loader'),

    resSection: document.getElementById('results-section'),
    resVendor: document.getElementById('res-vendor'),
    resDocnum: document.getElementById('res-docnum'),
    resDocdate: document.getElementById('res-docdate'),
    resConsignee: document.getElementById('res-consignee'),
    resShipper: document.getElementById('res-shipper'),
    tbody: document.getElementById('items-tbody'),
    btnImport: document.getElementById('btn-import'),
    loaderImport: document.getElementById('import-loader'),

    tbodyUnlisted: document.getElementById('unlisted-tbody'),

    tbodyAnalytics: document.getElementById('analytics-tbody'),
    analyticsSearch: document.getElementById('analytics-search'),
    analyticsDays: document.getElementById('analytics-days'),

    promptPresetSelect: document.getElementById('prompt-preset-select'),
    btnOpenPromptModal: document.getElementById('btn-open-prompt-modal'),
    promptModal: document.getElementById('prompt-modal'),
    btnClosePromptModal: document.getElementById('btn-close-prompt-modal'),
    modalPresetSelect: document.getElementById('modal-preset-select'),
    modalPresetName: document.getElementById('modal-preset-name'),
    btnNewPreset: document.getElementById('btn-new-preset'),
    btnDeletePreset: document.getElementById('btn-delete-preset'),
    btnResetDefaultPrompt: document.getElementById('btn-reset-default-prompt'),
    modalPromptTextarea: document.getElementById('modal-prompt-textarea'),
    promptCharCount: document.getElementById('prompt-char-count'),
    presetBadge: document.getElementById('preset-badge'),
    btnCancelPromptModal: document.getElementById('btn-cancel-prompt-modal'),
    btnSavePreset: document.getElementById('btn-save-preset'),

    tabBtnReconciliation: document.getElementById('tab-btn-reconciliation'),
    sectionReconciliation: document.getElementById('bugh-section-reconciliation'),
    reconcileDropZone: document.getElementById('reconcile-drop-zone'),
    reconcileFile: document.getElementById('reconcile-file-input'),
    btnReconcileParse: document.getElementById('btn-reconcile-parse'),
    reconcileLoader: document.getElementById('reconcile-loader'),
    reconcileResults: document.getElementById('reconcile-results'),
    reconcileTbody: document.getElementById('reconcile-tbody'),
};

let iikoCatalog = [];
let iikoSuppliers = [];
let currentToken = "";
let currentDocData = null;
let templateItems = [];
let analyticsCache = []; // Кэш для аналитики цен
let promptPresets = [];
let activePresetId = 0;
let currentCustomPrompt = "";
let reconciliationData = null;
let currentReconciliationFilter = 'all';

// ============================================================================
// 1. АВТОРИЗАЦИЯ И УПРАВЛЕНИЕ СЕССИЕЙ БУХГАЛТЕРА