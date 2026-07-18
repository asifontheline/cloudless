# Hosting this site on MilesWeb

This is a fully static site — one `index.html`, no server code, no database, no build step.

1. Log in to your MilesWeb cPanel.
2. Open **File Manager** → `public_html/` (or your domain's document root).
3. Upload `index.html` (or upload `cloudless-website.zip` and use Extract).
4. Done — the site is live at your domain. Enable **AutoSSL / Let's Encrypt** in cPanel for HTTPS.

Notes:
- Everything (styles, logo, favicon) is inlined; there are no other files to upload.
- To update: replace `index.html` and refresh.
- The live mesh console is a separate thing — it ships inside the node binary itself; this site is the
  public landing page linking to GitHub.
