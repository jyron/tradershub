package apiv1

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// mountSkillsDistribution exposes agent-skill packages as discoverable, no-auth
// downloads. Any directory under ./skills/<name>/ is served as both a
// browsable file root and a streamed tarball, so agent runtimes can install a
// skill with a one-line curl|tar without going through a third-party registry.
//
//	GET /skills                            → JSON index of available skills
//	GET /skills/<name>.tar.gz              → streamed gzipped tarball
//	GET /skills/<name>/<file-path>         → raw file inside the skill directory
func (h *handlers) mountSkillsDistribution(app *fiber.App) {
	app.Get("/skills", h.listSkills)
	// Single dispatch handler: Fiber's path param parser doesn't cleanly handle
	// dots in suffixes (e.g. `:name.tar.gz` matches `bottrade-benchmark`
	// instead of binding `.tar.gz`), so route on `/skills/*` and split the
	// segments in code. Order: tarball first, then file inside skill.
	app.Get("/skills/*", h.dispatchSkillRequest)
}

func (h *handlers) dispatchSkillRequest(c *fiber.Ctx) error {
	rest := c.Params("*")
	if rest == "" {
		return h.listSkills(c)
	}
	if strings.HasSuffix(rest, ".tar.gz") {
		name := strings.TrimSuffix(rest, ".tar.gz")
		return h.downloadSkillTarballByName(c, name)
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 || parts[1] == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "missing file path"})
	}
	return h.serveSkillFileByName(c, parts[0], parts[1])
}

const skillsRoot = "./skills"

func (h *handlers) listSkills(c *fiber.Ctx) error {
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	base := strings.TrimRight(h.AppBaseURL, "/")
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !validSkillName(name) {
			continue
		}
		out = append(out, map[string]any{
			"name":         name,
			"skill_md_url": base + "/skills/" + name + "/SKILL.md",
			"tarball_url":  base + "/skills/" + name + ".tar.gz",
			"readme_url":   base + "/skills/" + name + "/README.md",
		})
	}
	return c.JSON(fiber.Map{"skills": out})
}

func (h *handlers) serveSkillFileByName(c *fiber.Ctx, name, rel string) error {
	if !validSkillName(name) || !safeRelPath(rel) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid path"})
	}
	full := filepath.Join(skillsRoot, name, rel)
	if !pathInside(skillsRoot, full) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "path escapes skills root"})
	}
	return c.SendFile(full)
}

func (h *handlers) downloadSkillTarballByName(c *fiber.Ctx, name string) error {
	if !validSkillName(name) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid skill name"})
	}
	dir := filepath.Join(skillsRoot, name)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "skill not found"})
	}

	c.Set(fiber.HeaderContentType, "application/gzip")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s.tar.gz"`, name))
	c.Set("Cache-Control", "public, max-age=300")

	return c.SendStream(skillStream(dir))
}

// skillStream returns an io.Reader that lazily produces a gzipped tar archive
// of the named skill directory. Archive paths are rooted at the skill's own
// name (e.g. `bottrade-benchmark/SKILL.md`) so `tar -xzf` recreates the skill
// directly under whatever install root the caller chose.
func skillStream(dir string) io.Reader {
	r, w := io.Pipe()
	go func() {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		base := filepath.Base(dir)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(filepath.Join(base, rel))
			header.ModTime = time.Now().UTC()
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		})
		if err == nil {
			if err = tw.Close(); err == nil {
				err = gz.Close()
			}
		}
		_ = w.CloseWithError(err)
	}()
	return r
}

// validSkillName mirrors the SKILL.md `name:` lexical rule: lowercase letters,
// digits, hyphens. Prevents path traversal and surprise dotfile reads via URL.
func validSkillName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return false
	}
	return true
}

func safeRelPath(rel string) bool {
	if rel == "" {
		return false
	}
	if strings.HasPrefix(rel, "/") {
		return false
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func pathInside(root, candidate string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absCand, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absCand+string(filepath.Separator), absRoot+string(filepath.Separator)) || absCand == absRoot
}
