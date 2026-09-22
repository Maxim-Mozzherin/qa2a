function getAuthToken() {
    return localStorage.getItem('bugh_token') || "";
}
function checkAuthentication() {
    const token = getAuthToken();
    const isInvite = new URLSearchParams(window.location.search).has("invite");
    
    if (token) {
        if (els.loginModal) els.loginModal.classList.add("hidden");
        if (els.dashboardContent) els.dashboardContent.classList.remove("blur-sm", "pointer-events-none");
        initDashboard();
        if (typeof startGlobalBadgePolling === 'function') startGlobalBadgePolling();
    } else {
        if (!isInvite) {
            if (els.loginModal) els.loginModal.classList.remove("hidden");
        } else {
            if (els.loginModal) els.loginModal.classList.add("hidden");
        }
        if (els.dashboardContent) els.dashboardContent.classList.add("blur-sm", "pointer-events-none");
    }
}