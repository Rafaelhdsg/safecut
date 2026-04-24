# Landing Page — Deploy Guide

This folder contains the InfraMind landing page, served via GitHub Pages.

## Deploy to GitHub Pages

1. Go to **Settings → Pages** in the GitHub repo
2. Under **Source**, select **Deploy from a branch**
3. Choose branch `main`, folder `/docs`
4. Click **Save**

The page will be live at `https://rafaelhdsg.github.io/inframind-cli/` within a few minutes.

## Custom domain (inframind.io)

1. Register the domain and add a DNS CNAME record:
   - `inframind.io` → `rafaelhdsg.github.io`
   - Or for `www`: `www.inframind.io` → `rafaelhdsg.github.io`
2. The `CNAME` file in this folder is already configured for `inframind.io`
3. In **Settings → Pages → Custom domain**, enter `inframind.io` and enable **Enforce HTTPS**

## Waitlist form setup (Formspree)

1. Create a free account at [formspree.io](https://formspree.io)
2. Create a new form — you'll get an endpoint like `https://formspree.io/f/xABcDeFg`
3. Replace `YOUR_FORM_ID` in `index.html` with your form ID:
   ```
   <form ... action="https://formspree.io/f/xABcDeFg" method="POST">
   ```
4. Formspree free tier: 50 submissions/month, email notifications included

## Files

- `index.html` — Landing page (single file, no dependencies, no build step)
- `CNAME` — Custom domain config for GitHub Pages
