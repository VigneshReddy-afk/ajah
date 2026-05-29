# Deploying Ajah landing page

## Cloudflare Pages (recommended, free)

1. Push this repository to GitHub (already done).
2. Go to [Cloudflare Dashboard](https://dash.cloudflare.com) → **Workers & Pages** → **Create application** → **Pages**.
3. Connect your GitHub account and select the `ajah` repository.
4. Set **Build output directory** to `landing` (leave build command blank — no build step needed).
5. Click **Save and Deploy**. Cloudflare will publish the page and give you a URL like `ajah-abc123.pages.dev`.
6. To add the custom domain `useajah.com`, go to **Custom domains** on the Pages project and click **Set up a custom domain**.
7. Enter `useajah.com` and click **Continue**.
8. Log in to **Hostinger** → **Domains** → **useajah.com** → **DNS / Nameservers**.
9. Add a CNAME record:
   - **Type:** CNAME
   - **Name:** `@` (or leave blank for root)
   - **Target:** `your-project.pages.dev`
   - **TTL:** Auto

   > Cloudflare Pages handles the SSL certificate automatically once DNS propagates (usually under 5 minutes when your domain uses Cloudflare nameservers).

---

## Local preview

No build step is required. Open the file directly in any browser:

```
open landing/index.html        # macOS
start landing/index.html       # Windows
xdg-open landing/index.html    # Linux
```

Or serve it with any static file server:

```bash
npx serve landing
# → http://localhost:3000
```
