// Makes photo.jpg, an 800×600 JPEG that isn't from a phone, for the checks
// that send one: Chromium draws it and takes it as a screenshot.
//
//   ./node browser/make-photo.mjs FILE

import { launch } from './browsers.mjs';

const [, , file] = process.argv;
const page = `<body style="margin:0;height:100vh;background:linear-gradient(135deg,#f6c453,#d9480f 45%,#5f3dc4);
display:grid;place-items:center;font:bold 96px sans-serif;color:#fff">Dens</body>`;
const b = await launch('chromium', 9290, { size: [800, 600] });
try {
    await b.go(`data:text/html,${encodeURIComponent(page)}`);
    await b.shot(file, 'jpeg');
} finally {
    await b.close();
}
