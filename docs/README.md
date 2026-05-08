# Landing Page — Deploy Guide

This folder contains the InfraMind landing page, served via GitHub Pages.

## Deploy to GitHub Pages

1. Go to **Settings → Pages** in the GitHub repo
2. Under **Source**, select **Deploy from a branch**
3. Choose branch `main`, folder `/docs`
4. Click **Save**

The page will be live at `https://rafaelhdsg.github.io/inframind-cli/` within a few minutes.

## Custom domain

The site is currently served from the default GitHub Pages URL
(`rafaelhdsg.github.io/inframind-cli`). When a final brand domain is
ready, the steps to wire it up are:

1. Register the domain at any registrar (Cloudflare Registrar, Porkbun
   and Namecheap are good options for `.io`).
2. At the registrar's DNS panel, add either:
   - **Apex** (`example.com`): four A records pointing at GitHub Pages —
     `185.199.108.153`, `185.199.109.153`, `185.199.110.153`,
     `185.199.111.153`. Optionally add the matching AAAA records for
     IPv6 (`2606:50c0:8000::153` … `8003::153`).
   - **Subdomain** (`www.example.com` / `cli.example.com`): one CNAME
     record pointing at `rafaelhdsg.github.io`.
3. Drop a single-line `CNAME` file in this folder containing the chosen
   hostname (e.g. `cli.example.com`) and commit it.
4. In **Settings → Pages → Custom domain**, enter the same hostname.
   Wait for the DNS check to pass (usually 5–30 minutes), then tick
   **Enforce HTTPS** so GitHub provisions a Let's Encrypt certificate.

## Waitlist form (Formspree)

The waitlist form in `index.html` is already wired to a Formspree
endpoint owned by the project. Formspree free tier covers 50
submissions/month with email notifications.

If you fork the project and want submissions to land in your own
inbox, replace the form ID in `index.html`:

```html
<form ... action="https://formspree.io/f/<your-form-id>" method="POST">
```

Recommended Formspree settings (free tier): enable **Submission
Archive** and turn on **CAPTCHA** under Settings → Spam Protection.
Allowed-domain restrictions are paid-only, so CAPTCHA is the main
defence against drive-by spam on the form ID.

## Files

- `index.html` — Landing page (single file, no dependencies, no build step)
- `CNAME` — Custom domain config for GitHub Pages
