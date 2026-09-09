function getAuthToken() {
    return localStorage.getItem('bugh_token') || "";
}
function checkAuthentication() {
    const token = getAuthToken();
    if (token) {
        if (els.loginModal) els.loginModal.classList.add('hidden');
        if (els.dashboardContent) els.dashboardContent.classList.remove('blur-sm', 'pointer-events-none');
        initDashboard();
    } else {
        if (els.loginModal) els.loginModal.classList.remove('hidden');
        if (els.dashboardContent) els.dashboardContent.classList.add('blur-sm', 'pointer-events-none');
    }
}