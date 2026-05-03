# Reads baseline

Measured 2026-05-02 against `your-forge.example.com`, repo
`Gerwood/gaia`, PR #75 (no comments), and `cli/cli` on github.com
(release rows). Token estimates use `bytes / 4`.

| Command                                            | Bytes   | ≈Tokens | vs. raw curl |
|----------------------------------------------------|---------|---------|--------------|
| `gaia whoami`                                      | 138     | 34      | **0.21×**    |
| `curl /user`                                       | 647     | 161     | 1×           |
| | | | |
| `gaia issue list` (default, 30 issues)             | 22 639  | 5 659   | **0.35×**    |
| `gaia --fields number,title,state issue list`      | 3 734   | 933     | **0.06×**    |
| `tea issues list --output simple`                  | 3 452   | 863     | 0.05×        |
| `curl /issues?type=issues&state=open&limit=30`     | 65 298  | 16 324  | 1×           |
| | | | |
| `gaia issue view 1`                                | 3 625   | 906     | **0.73×**    |
| `tea issues 1`                                     | 7 772   | 1 943   | 1.57×        |
| `curl /issues/1`                                   | 4 952   | 1 238   | 1×           |
| | | | |
| `gaia pr list` (default, all 30)                   | 44 529  | 11 132  | **0.29×**    |
| `gaia --fields number,title,state,head.ref,base.ref pr list` | 4 491 | 1 122 | **0.03×** |
| `tea pulls list --output simple`                   | 1 954   | 488     | 0.01×        |
| `curl /pulls?state=all&limit=30`                   | 153 942 | 38 485  | 1×           |
| | | | |
| `gaia pr view 75` (no CI)                          | 4 255   | 1 063   | **0.39×**    |
| `gaia pr view 75 --with-ci`                        | 4 387   | 1 096   | **0.40×**    |
| `tea pulls 75`                                     | 7 334   | 1 833   | 0.67×        |
| `curl /pulls/75`                                   | 10 983  | 2 745   | 1×           |
| | | | |
| `gaia pr diff 75` (full structured)                | 111 947 | 27 986  | 1.61×        |
| `gaia --fields path,status pr diff 75`             | 1 620   | 405     | **0.02×**    |
| `curl /pulls/75.diff` (raw text)                   | 69 360  | 17 340  | 1×           |
| | | | |
| `gaia pr comments 75` (empty thread)               | 45      | 11      | **0.38×**    |
| 3× curl (issue/reviews/comments)                   | 118     | 29      | 1×           |
| | | | |
| `gaia search MVP` (1 result)                       | 204     | 51      | **0.04×**    |
| `gaia --fields kind,number,title,repo search MVP`  | 204     | 51      | **0.04×**    |
| `curl /issues?q=MVP&limit=30`                      | 4 954   | 1 238   | 1×           |
| | | | |
| `gaia release list` (5, vs api.github.com cli/cli) | 21 616  | 5 404   | **0.08×**    |
| `gaia --fields tag_name,name,prerelease release list` | 606  | 151     | **0.002×**   |
| `curl /releases?per_page=5` (cli/cli)              | 258 218 | 64 554  | 1×           |
| | | | |
| `gaia release view v2.79.0` (cli/cli)              | 2 958   | 739     | **0.06×**    |
| `curl /releases/tags/v2.79.0` (cli/cli)            | 48 555  | 12 138  | 1×           |
