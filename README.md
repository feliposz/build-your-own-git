# Build Your Own Git

This my implementation in Go for the
["Build Your Own Git" Challenge](https://codecrafters.io/challenges/git).

This is a small Git implementation that's capable of
initializing a repository, creating commits and cloning a public repository.
It's capable of handling the `.git` directory, Git objects (blobs,
commits, trees etc.), Git's transfer protocols and more.

**Note**: Head over to
[codecrafters.io](https://codecrafters.io) to try the challenge yourself.

# Testing locally

The `your_git.sh` script is expected to operate on the `.git` folder inside the
current working directory. If you're running this inside the root of this
repository, you might end up accidentally damaging your repository's `.git`
folder.

We suggest executing `your_git.sh` in a different folder when testing locally.
For example:

```sh
mkdir -p /tmp/testing && cd /tmp/testing
/path/to/your/repo/your_git.sh init
```

To make this easier to type out, you could add a
[shell alias](https://shapeshed.com/unix-alias/):

```sh
alias mygit=/path/to/your/repo/your_git.sh

mkdir -p /tmp/testing && cd /tmp/testing
mygit init
```
