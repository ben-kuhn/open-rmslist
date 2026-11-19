/* Resolve the latest release's version and direct download links. Falls back to
   the release page if the GitHub API is unavailable or rate-limited. */
(() => {
  const releasePage = "https://github.com/ben-kuhn/rms-radar/releases/latest";
  const api = "https://api.github.com/repos/ben-kuhn/rms-radar/releases/latest";

  for (const el of document.querySelectorAll("[data-latest-version]")) {
    el.textContent = "latest";
  }

  fetch(api, { headers: { Accept: "application/vnd.github+json" } })
    .then((res) => (res.ok ? res.json() : null))
    .then((release) => {
      if (!release) return;
      for (const el of document.querySelectorAll("[data-latest-version]")) {
        el.textContent = release.tag_name || "latest";
      }
      const assets = Array.isArray(release.assets) ? release.assets : [];
      for (const link of document.querySelectorAll("[data-asset-prefix]")) {
        const prefix = link.dataset.assetPrefix || "";
        const suffix = link.dataset.assetSuffix || "";
        const asset = assets.find((a) =>
          a.name.startsWith(prefix) && a.name.endsWith(suffix));
        if (asset && asset.browser_download_url) {
          link.href = asset.browser_download_url;
          link.dataset.resolved = "true";
        }
      }
    })
    .catch(() => {});
})();
