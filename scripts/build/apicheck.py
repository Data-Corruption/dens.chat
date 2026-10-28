"""Checks the Android app's shell for framework calls newer than the oldest
Android it runs on, as Android Lint's NewApi check would in a Gradle build
(M12): such a call throws NoSuchMethodError there unless a check of
Build.VERSION.SDK_INT guards it.

It lists every reference in the shell's classes to a framework member that
arrived after MIN_SDK, from the SDK's own API database, and fails on any that
ALLOWED doesn't list: the newer calls the shell makes, each under such a
check. A call on one of the shell's own classes, as an activity calling what
it inherits, counts as one on the framework class it extends. Constants
don't count, since the compiler copies them in.

Usage: python3 -I apicheck.py API_VERSIONS_XML CLASSES_JAR MIN_SDK ALLOWED
"""
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

api_xml, jar, min_sdk, allowed_path = sys.argv[1], sys.argv[2], float(sys.argv[3]), sys.argv[4]

root = ET.parse(api_xml).getroot()
classes = {}
supers = {}
for c in root.iter('class'):
    members = {}
    for m in c:
        if m.tag in ('method', 'field') and 'since' in m.attrib:
            members[m.get('name')] = float(m.get('since'))
    classes[c.get('name')] = (float(c.get('since', '1')), members)
    supers[c.get('name')] = [e.get('name') for e in c if e.tag in ('extends', 'implements')]


def since(owner, name):
    """The API level a member arrived at, looking up through its supertypes."""
    seen = set()
    todo = [owner]
    while todo:
        cls = todo.pop(0)
        if cls in seen or cls not in classes:
            continue
        seen.add(cls)
        cls_since, members = classes[cls]
        if name in members:
            return max(cls_since, members[name])
        todo.extend(supers.get(cls, []))
    return classes.get(owner, (0, {}))[0]


allowed = set()
with open(allowed_path) as f:
    for line in f:
        line = line.split('#', 1)[0].strip()
        if line:
            allowed.add(line)

names = subprocess.run(['unzip', '-Z1', jar], capture_output=True, text=True, check=True).stdout.split()
names = [n[:-6].replace('/', '.') for n in names if n.endswith('.class')]
out = subprocess.run(['javap', '-c', '-p', '-classpath', jar] + names, capture_output=True, text=True, check=True).stdout
# The shell's own classes, and what each extends and implements.
header = re.compile(r'\b(?:class|interface) ([\w.$]+)(?: extends ([\w.$, ]+?))?(?: implements ([\w.$, ]+))? \{$')
ours = {}
for line in out.splitlines():
    m = header.search(line.rstrip())
    if m:
        parents = ','.join(p for p in m.groups()[1:] if p)
        ours[m.group(1).replace('.', '/')] = [p.strip().replace('.', '/') for p in parents.split(',') if p.strip()]


def framework(owner):
    """The framework classes an owner stands for: itself, or for one of the
    shell's classes, the framework classes above it."""
    if owner.startswith('android/'):
        return [owner]
    seen, todo, found = set(), [owner], []
    while todo:
        cls = todo.pop(0)
        if cls in seen:
            continue
        seen.add(cls)
        if cls.startswith('android/'):
            found.append(cls)
        else:
            todo.extend(ours.get(cls, []))
    return found


# javap names no class in a reference to the class it shows.
ref = re.compile(r'// (Method|InterfaceMethod|Field) (?:([\w/$]+)\.)?([\w<>$"]+):(\S+)')
current = ''
found = {}
for line in out.splitlines():
    m = header.search(line.rstrip())
    if m:
        current = m.group(1)
        continue
    m = ref.search(line)
    if not m:
        continue
    kind, owner, name, desc = m.groups()
    owner = owner or current.replace('.', '/')
    key = name + desc if kind != 'Field' else name
    for base in framework(owner):
        s = since(base, key)
        if s > min_sdk:
            found.setdefault((base.replace('/', '.') + '.' + key, s), set()).add(current.split('$')[0])

unguarded = 0
for (member, s), users in sorted(found.items(), key=lambda x: (-x[0][1], x[0][0])):
    listed = member in allowed
    if not listed:
        unguarded += 1
    print(f'{"listed" if listed else "NEW   "} API {s:g}: {member}  (in {", ".join(sorted(users))})')
for member in sorted(allowed - {m for m, _ in found}):
    print(f'unused listing: {member}')
if unguarded:
    print(f'{unguarded} newer calls not in {allowed_path}: guard each with a check of Build.VERSION.SDK_INT, then list it there')
    sys.exit(1)
