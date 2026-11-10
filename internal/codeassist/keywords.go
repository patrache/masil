package codeassist

import "strings"

// Keyword and builtin tables of the LeetCode languages (the completion
// candidates that are not in a buffer). Small and static on purpose: no LSP.
var keywordTables = map[string]string{
	"go": `break case chan const continue default defer else fallthrough for func go goto if import
		interface map package range return select struct switch type var
		append cap clear close complex copy delete imag len make max min new panic print println real recover
		any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64
		rune string uint uint8 uint16 uint32 uint64 uintptr true false iota nil`,
	"python3": `False None True and as assert async await break case class continue def del elif else
		except finally for from global if import in is lambda match nonlocal not or pass raise return
		try while with yield self
		abs all any bin bool bytes callable chr dict dir divmod enumerate filter float format frozenset
		getattr hasattr hash hex id input int isinstance issubclass iter len list map max min next object
		oct open ord pow print range repr reversed round set setattr slice sorted str sum super tuple type zip`,
	"python": `False None True and as assert break class continue def del elif else except exec finally
		for from global if import in is lambda not or pass print raise return try while with yield self
		abs all any basestring bin bool cmp dict dir divmod enumerate filter float format getattr hasattr
		hash hex id int isinstance iter len list long map max min next object oct open ord pow range
		raw_input reduce repr reversed round set sorted str sum super tuple type unichr unicode xrange zip`,
	"c": `auto break case char const continue default do double else enum extern float for goto if
		inline int long register restrict return short signed sizeof static struct switch typedef union
		unsigned void volatile while bool true false NULL
		malloc calloc realloc free memset memcpy memmove strlen strcmp strncmp strcpy strncpy strcat
		printf sprintf snprintf scanf qsort abs size_t INT_MAX INT_MIN`,
	"cpp": `alignas alignof and auto bool break case catch char class const constexpr const_cast continue
		decltype default delete do double dynamic_cast else enum explicit export extern false float for
		friend goto if inline int long mutable namespace new noexcept not nullptr operator or private
		protected public register reinterpret_cast return short signed sizeof static static_assert
		static_cast struct switch template this throw true try typedef typeid typename union unsigned
		using virtual void volatile while
		std vector string map unordered_map set unordered_set multiset pair queue priority_queue stack
		deque list array sort reverse max min swap make_pair push_back pop_back emplace_back begin end
		size empty insert erase find count lower_bound upper_bound accumulate INT_MAX INT_MIN cout endl`,
	"java": `abstract assert boolean break byte case catch char class const continue default do double
		else enum extends final finally float for goto if implements import instanceof int interface long
		native new package private protected public record return short static strictfp super switch
		synchronized this throw throws transient try var void volatile while true false null
		String Integer Long Double Character Boolean Math List ArrayList LinkedList Map HashMap TreeMap
		Set HashSet TreeSet Queue Deque ArrayDeque PriorityQueue Stack Arrays Collections StringBuilder
		Object System length size add get put remove contains containsKey isEmpty`,
	"csharp": `abstract as async await base bool break byte case catch char checked class const continue
		decimal default delegate do double else enum event explicit extern false finally fixed float for
		foreach get goto if implicit in int interface internal is lock long namespace new null object
		operator out override params private protected public readonly ref return sbyte sealed set short
		sizeof stackalloc static string struct switch this throw true try typeof uint ulong unchecked
		unsafe ushort using var virtual void volatile while yield
		List Dictionary HashSet Queue Stack PriorityQueue Math String StringBuilder Array Console
		Count Length Add Remove Contains ContainsKey`,
	"javascript": `async await break case catch class const continue debugger default delete do else
		export extends false finally for function if import in instanceof let new null of return super
		switch this throw true try typeof undefined var void while with yield
		Array Object String Number Math Map Set JSON console Infinity NaN parseInt parseFloat length
		push pop shift unshift slice splice sort map filter reduce forEach indexOf includes join`,
	"typescript": `abstract any as async await boolean break case catch class const continue debugger
		declare default delete do else enum export extends false finally for function if implements
		import in instanceof interface is keyof let namespace never new null number of private protected
		public readonly return string super switch symbol this throw true try type typeof undefined
		unknown var void while yield
		Array Object String Number Math Map Set JSON Record Partial console Infinity NaN parseInt length
		push pop shift unshift slice splice sort map filter reduce forEach indexOf includes join`,
	"php": `abstract and array as break callable case catch class clone const continue declare default
		do echo else elseif empty extends final finally fn for foreach function global goto if implements
		include instanceof insteadof interface isset list match namespace new null or print private
		protected public readonly require return static switch throw trait try unset use var while xor
		yield true false self parent
		count strlen str_split array_push array_pop array_keys array_values array_map array_filter
		in_array sort usort max min abs intval strval implode explode substr`,
	"swift": `associatedtype class deinit enum extension fileprivate func import init inout internal let
		open operator private protocol public rethrows static struct subscript typealias var break case
		continue default defer do else fallthrough for guard if in repeat return switch where while as
		catch false is nil super self Self throw throws true try async await
		Int Double String Bool Character Array Dictionary Set Optional print min max abs count append
		isEmpty sorted map filter reduce`,
	"kotlin": `as break by catch class companion constructor continue data do else enum false finally
		for fun get if import in init inline interface internal is lateinit null object open override
		package private protected public return sealed set super this throw true try typealias val var
		when while
		Int Long Double Boolean Char String Array IntArray List MutableList Map MutableMap Set MutableSet
		listOf mutableListOf mapOf mutableMapOf setOf mutableSetOf arrayOf intArrayOf println maxOf minOf
		size indices until downTo step`,
	"rust": `as async await break const continue crate dyn else enum extern false fn for if impl in let
		loop match mod move mut pub ref return self Self static struct super trait true type unsafe use
		where while
		i32 i64 u32 u64 usize isize f64 bool char str String Vec Option Some None Result Ok Err Box
		HashMap HashSet BTreeMap VecDeque BinaryHeap println vec len push pop iter collect unwrap clone`,
	"ruby": `BEGIN END alias and begin break case class def do else elsif end ensure false for if in
		module next nil not or redo rescue retry return self super then true undef unless until when
		while yield
		puts print require attr_accessor attr_reader each map select reject reduce length size push pop
		sort Array Hash String Integer`,
	"scala": `abstract case catch class def do else enum extends false final finally for forSome given
		if implicit import lazy match new null object override package private protected return sealed
		super then this throw trait true try type using val var while with yield
		Int Long Double Boolean Char String Array List Map Set Seq Vector Option Some None println math
		length size`,
}

var keywordCache = map[string][]string{}

func init() {
	for lang, s := range keywordTables {
		keywordCache[lang] = strings.Fields(s)
	}
}

// Keywords are lang's keywords and common builtins (nil for a language
// without a table). The slice is shared: do not change it.
func Keywords(lang string) []string { return keywordCache[lang] }
