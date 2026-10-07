// Leads Monster: API Client
const getBasePath = () => {
    return window.location.pathname.startsWith('/bugh-team/parser') ? '/bugh-team/parser' : '';
};

const API = {
    async request(endpoint, options = {}) {
        const basePath = getBasePath();
        const fullUrl = endpoint.startsWith('http') ? endpoint : (basePath + endpoint);
        const defaultHeaders = {
            'Accept': 'application/json',
        };

        if (options.body && typeof options.body === 'object' && !(options.body instanceof FormData)) {
            defaultHeaders['Content-Type'] = 'application/json';
            options.body = JSON.stringify(options.body);
        }

        options.headers = {
            ...defaultHeaders,
            ...options.headers,
        };

        const res = await fetch(fullUrl, options);

        if (res.status === 401) {
            // Unauthenticated session, redirect to login
            const loginPath = basePath ? `${basePath}/login` : '/login';
            if (!window.location.pathname.includes('/login')) {
                window.location.href = loginPath;
            }
            throw new Error('Сессия истекла. Пожалуйста, выполните вход.');
        }

        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
            throw new Error(data.error || `Ошибка сервера (${res.status})`);
        }

        return data;
    },

    async getMe() {
        return this.request('/api/auth/me');
    },

    async logout() {
        const basePath = getBasePath();
        await this.request('/api/auth/logout', { method: 'POST' });
        window.location.href = basePath ? `${basePath}/login` : '/login';
    },

    async getLeads(filters = {}) {
        const params = new URLSearchParams();
        for (const key in filters) {
            if (filters[key] !== undefined && filters[key] !== null && filters[key] !== '' && filters[key] !== 'all') {
                if (Array.isArray(filters[key])) {
                    if (filters[key].length > 0) {
                        params.set(key, filters[key].join(','));
                    }
                } else {
                    params.set(key, filters[key]);
                }
            }
        }
        return this.request(`/api/leads?${params.toString()}`);
    },

    async updateLead(id, data) {
        return this.request(`/api/leads/${id}`, {
            method: 'PATCH',
            body: data
        });
    },

    async startScraper(opts) {
        return this.request('/api/scraper/start', {
            method: 'POST',
            body: opts
        });
    },

    async stopScraper() {
        return this.request('/api/scraper/stop', {
            method: 'POST'
        });
    },

    async getScraperStatus() {
        return this.request('/api/scraper/status');
    },

    getExportURL(filters = {}) {
        const basePath = getBasePath();
        const params = new URLSearchParams();
        for (const key in filters) {
            if (filters[key] !== undefined && filters[key] !== null && filters[key] !== '' && filters[key] !== 'all') {
                if (key === 'page' || key === 'page_size') continue;
                if (Array.isArray(filters[key])) {
                    if (filters[key].length > 0) {
                        params.set(key, filters[key].join(','));
                    }
                } else {
                    params.set(key, filters[key]);
                }
            }
        }
        return `${basePath}/api/leads/export?${params.toString()}`;
    }
};
