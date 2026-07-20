{
					__sveltekit_1oo58s9 = {
						base: ""
					};

					const element = document.currentScript.parentElement;

					Promise.all([
						import("/_app/immutable/entry/start.CPPHza41.js"),
						import("/_app/immutable/entry/app.iiRQqm9l.js")
					]).then(([kit, app]) => {
						kit.start(app, element);
					});
				}
			