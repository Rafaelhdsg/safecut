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
