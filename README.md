# Kobo

This repository contains a simple tool which makes using [kobopatch] and [kobopatch-patches] much simpler.

Just edit the `overrides.yaml` file and run `go run main.go` and the tool will take care of downloading the right firmare file, building `kobopatch` for your platform, preparing the patch templates and then creating a patched firmware.

I initially created this because I found using [kobopatch-patches] a little cumbersome and it required a fair amount of manual work, now with this tool, all that's needed is:

```bash
# To download base firmware, prepare patch files and build a patched firmware:
go run main.go -uuid $UUID_OF_YOUR_KOBO_MODEL -version $DESIRED_VERSION_TO_PATCH
```

That's it! 🎉

## Usage

```txt
Usage: go run main.go [options]

Options:
  -uuid string
        uuid of the kobo (see firmwares.json) (default "00000000-0000-0000-0000-000000000370")
  -version string
        version of the patch to create (default "4.19.14123")

```

Have a look at the [firmwares.json] file to find the right uuid (and available versions) for your Kobo.

[kobopatch]: https://github.com/geek1011/kobopatch
[kobopatch-patches]: https://github.com/geek1011/kobopatch-patches
[firmwares.json]: ./firmwares.json
