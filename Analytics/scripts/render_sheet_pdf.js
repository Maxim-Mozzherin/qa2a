const puppeteer = require('puppeteer-core');
const fs = require('fs');

const restId = process.argv[2] || '1';
const outputPath = process.argv[3] || '/tmp/rendered_report.pdf';
const token = process.argv[4] || 'a4f91c83e2b74059d81e3a6c905b7f14e2d83b9c';

(async () => {
    let browser;
    try {
        browser = await puppeteer.launch({
            executablePath: '/usr/bin/google-chrome',
            headless: true,
            args: [
                '--no-sandbox',
                '--disable-setuid-sandbox',
                '--disable-dev-shm-usage',
                '--disable-gpu',
                '--headless=new'
            ]
        });

        const page = await browser.newPage();
        await page.setViewport({ width: 1200, height: 1600, deviceScaleFactor: 2 });

        const url = `http://127.0.0.1:8098/analytics/sheet?restaurant_id=${restId}&token=${token}`;
        await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });

        // Ждем флага готовности отчетов и Chart.js
        await page.waitForFunction(() => window.REPORT_READY === true, { timeout: 20000 }).catch(() => {
            console.warn('Wait for REPORT_READY timed out, continuing anyway...');
        });

        // Небольшая пауза 200ms для фиксации шрифтов
        await new Promise(r => setTimeout(r, 200));

        await page.pdf({
            path: outputPath,
            format: 'A4',
            printBackground: true,
            margin: {
                top: '6mm',
                bottom: '6mm',
                left: '6mm',
                right: '6mm'
            }
        });

        await browser.close();
        console.log('SUCCESS');
        process.exit(0);
    } catch (err) {
        if (browser) await browser.close();
        console.error('Render error:', err);
        process.exit(1);
    }
})();
