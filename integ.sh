#!/bin/sh

for br in $(git log --oneline --first-parent ..next | cut -d ' ' -f 4 | sed -e "s/'//g" | tac)
do
	echo "==> Merge $br"
	git merge --no-ff --log --no-edit $br
done
git diff next
