# dedup

Dedup is a simple command line program to remove duplicate files.

```
Usage of dedup:
  -x    Execute. The default is dry-run, which prints every duplicate file to stdout.
  -v    Enable verbose logging
  -vvv
        Enable debug-level logging
```

## ⚠️ IMPORTANT ⚠️

Do a dry run to avoid surprises.

Do _not_ script this program unless you are very sure of the directory contents or have very good backups.

For example,
Windows has hidden configuration files, such as desktop.ini, which could be caught in a duplicate comparison if the contents were identical.

Files below 2KB are skipped,
which should prevent most configuration files from getting caught.

If you need more complex file name/extension filtering,
pipe the dry-run results through programs like `grep`.

## Example Usage

By default, dedup runs on the current working directory:

```bash
cd ~/Downloads && ./dedup.exe
```

dedup accepts a list of directories:

```bash
# git-bash on windows
./dedup.exe ~/Downloads /D/Downloads /F/Downloads
```

There is no name or path filtering built in to dedup.
If the dry run includes directories or file extensions that you want to skip,
then pipe the results of a dry run through a program such as grep to filter the results:

```bash
# an example of filtering out files matching *.ini
# grep -v: invert (look for lines not matching)
cd ~/Pictures && dedup.exe | grep -v \.ini$
```

To see which files are paired as duplicates,
run dedup with the `-v` flag:

```bash
# send info-level logs (which includes file comparison results) to stderr
# these are not in machine-readable form and the format may change
./dedup.exe -v ~/Downloads
```

Use the `-x` flag to execute and remove duplicate files.
Removed files will _NOT_ be in the recycle bin.
Nothing will be printed to stdout.

```bash
# this will REMOVE duplicate files
./dedup.exe -x ~/Downloads
```
